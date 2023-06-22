package operations

import (
	"context"
	"errors"
	"fmt"
	"github.com/aalpar/deheap"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/lib/pool"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ResultChunk struct {
	buf        []byte
	index      int
	start, end int64
}
type chunkHeap struct {
	mu     *sync.Mutex
	chunks []ResultChunk
}

// Len is the number of elements in the collection.
func (h chunkHeap) Len() int {
	return len(h.chunks)
}

func (h chunkHeap) Less(i int, j int) bool {
	//return h.chunks[i].index < h.chunks[j].index
	return h.chunks[i].start < h.chunks[j].start
}

func (h chunkHeap) Swap(i int, j int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chunks[i], h.chunks[j] = h.chunks[j], h.chunks[i]
}

func (h *chunkHeap) Push(x any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chunks = append(h.chunks, x.(ResultChunk))
}

func (h *chunkHeap) Pop() any {
	h.mu.Lock()
	defer h.mu.Unlock()
	old := h.chunks
	n := len(old)
	x := old[n-1]
	h.chunks = old[0 : n-1]
	return x
}
func (h *chunkHeap) SyncCall(fun func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fun()
}

func safeChanClose[T any](ch chan T) (justClosed bool) {
	defer func() {
		if recover() != nil {
			// The return result can be altered
			// in a defer function call.
			justClosed = false
		}
	}()

	// assume ch != nil here.
	close(ch)   // panic if ch is closed
	return true // <=> justClosed = true; return
}

func safeChanSend[T any](ch chan T, value T) (succ bool) {
	defer func() {
		if recover() != nil {
			succ = false
		}
	}()

	ch <- value // panic if ch is closed
	return true // <=> closed = false; return
}

type DownloadPart struct {
	chunkI     int
	start, end int64
}

func splitDownloadChunks(srcSize int64, CHUNK_SIZES []int64) (chunkTasks []DownloadPart) {
	chunkTasks = []DownloadPart{}
	var curpos int64 = 0
	curI := 0
	for _, chunkSize := range CHUNK_SIZES {
		end := curpos + int64(chunkSize)
		if end > srcSize {
			end = srcSize
		}
		chunkTasks = append(chunkTasks, DownloadPart{
			chunkI: curI,
			start:  curpos, end: end}) // first part - 1M
		curI++
		curpos = end
	}
	if curpos < srcSize {
		lastChunkSize := CHUNK_SIZES[len(CHUNK_SIZES)-1]
		for {
			end := curpos + int64(lastChunkSize)
			if end > srcSize {
				end = srcSize
			}
			chunkTasks = append(chunkTasks, DownloadPart{
				chunkI: curI,
				start:  curpos, end: end}) // first part - 1M
			curI++
			curpos = end
			if end == srcSize {
				break
			}
		}
	}
	return
}

// Copy src to (f, remote) using streams download threads and the OpenWriterAt feature
func multiThreadCopyChunked(ctx context.Context, f fs.Fs, remote string, src fs.Object, streams int, tr *accounting.Transfer) (newDst fs.Object, err error) {
	var _err error
	_ = _err // helper err variable

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if src.Size() < 0 {
		return nil, errors.New("multi-thread copy: can't copy unknown sized file")
	}
	if src.Size() == 0 {
		return nil, errors.New("multi-thread copy: can't copy zero sized file")
	}
	ci := fs.GetConfig(ctx)

	// 1. Prepare various parameter
	//      (chunkSizes, pre-warm link)
	//
	CHUNK_SIZES := []int64{}
	for _, sizeStr := range strings.Split(ci.MultiThreadChunkSize, ",") {
		var size fs.SizeSuffix
		if err := size.Set(sizeStr); err != nil {
			fs.Errorf(src, "Invalid chunk size item: %s (%v)", size, err)
			return nil, err
		}
		CHUNK_SIZES = append(CHUNK_SIZES, int64(size))
	}

	totalSize := src.Size()
	chunkTasks := splitDownloadChunks(totalSize, CHUNK_SIZES)
	fs.Debugf(src, "final chunks split: %v", chunkTasks)
	totalChunks := len(chunkTasks)

	// pre-warm the download link cache
	firstRc, err := NewReOpen(ctx, src, ci.LowLevelRetries, &fs.RangeOption{Start: 0, End: int64(CHUNK_SIZES[0]) - 1})
	if err != nil {
		return nil, err
	}
	defer fs.CheckClose(firstRc, &err)
	// we read the body later to allow other goroutines run first

	// 2. Prepare various runtime states (pipes, chans)
	//
	// The whole stream of this function is
	//      ---> taskPutter ---(taskQueue)-->  streamDownloader
	//      \                                       /
	//    (sync with curChunk)               (resultChan)
	//        \                                 /
	//         -----      writeLoop     <------
	//                      |
	//               (pipe to reader)
	//                      |
	//                 f.Put call
	//
	//

	reader, writer := io.Pipe()
	// must close writer here, or goroutine leaks & tr.Accouting will stuck
	defer fs.CheckClose(writer, &err) // also close here, just in case

	stopped := false                     // stop flag
	errChan := make(chan error)          // chan for receiving error (from all goroutine)
	workQueue := make(chan DownloadPart) // chan for sending tasks (taskPutter -> streamDownloader)
	resultChan := make(chan ResultChunk) // chan for receiving download result (streamDownloader -> writeLoop)

	// 3. streamDownloader part, download a chunk as per taskPutter's instruction (<-workQueue DownloadPart)
	//      which contains start, end, and chunkIndex
	//      sends data to writeLoop with resultChan
	maxChunkSize := int64(0)
	for _, size := range CHUNK_SIZES {
		if size > maxChunkSize {
			maxChunkSize = size
		}
	}

	multiThreadChunkPool := pool.New(
		10*time.Second, int(maxChunkSize),
		0, // we changed to zero to release chunk ASAP
		true)
	defer multiThreadChunkPool.Flush()
	releaseBuf := func(buf []byte, wait bool) {
		if cap(buf) == int(maxChunkSize) {
			multiThreadChunkPool.Put(buf)
		}
	}

	var waitingThread int32
	streamDownloadChunk := func(stream int, start, end int64, chunkI int) bool {
		fs.Debugf(src, "multi-thread copy start: stream %d, chunk %d(%v) size %v", stream+1, chunkI, fs.SizeSuffix(start), fs.SizeSuffix(end-start))

		startTime := time.Now()

		rc, err := NewReOpen(ctx, src, ci.LowLevelRetries, &fs.RangeOption{Start: start, End: end - 1})
		if err != nil {
			safeChanSend(errChan, fmt.Errorf("multipart copy: failed to open source: %v", err))
			return true
		}

		var buf []byte
		if end-start == maxChunkSize {
			// MUST ENSURE BUF ARE PUT BACK, OR mmap LEAKS
			buf = multiThreadChunkPool.Get()
		} else {
			buf = make([]byte, end-start)
		}
		oribuf := buf // keep a reference to original buffer, as we may slice them afterwards
		_ = oribuf

		// Main read loop. NEVER RETURN OUT HERE!
		curReadOff := start
		minRead := int64(3 * 1024 * 1024)
		for {
			curReadEnd := curReadOff + minRead
			if curReadEnd > end {
				curReadEnd = end
			}
			if curReadOff == curReadEnd {
				// we're done
				break
			}
			//fs.Debugf(src, "multi-thread copy: downloading trying to read buf %v-%v", curReadOff, curReadEnd)
			n, err := io.ReadFull(rc, buf[curReadOff-start:curReadEnd-start])
			curReadOff += int64(n)
			if err != nil {
				break
			}
			if curWaiting := atomic.LoadInt32(&waitingThread); curWaiting >= 1 {
				if end-curReadOff > minRead {
					// double splitting
					newChunkSize := (end - curReadOff) / 2
					splitPoint := curReadOff + newChunkSize
					fs.Debugf(src, "multi-thread copy: re-split chunk %d into %v-%v-%v, curChunk downloaded %v", chunkI, fs.SizeSuffix(start), fs.SizeSuffix(splitPoint), fs.SizeSuffix(end), fs.SizeSuffix(curReadOff-start))
					go safeChanSend(workQueue, DownloadPart{
						chunkI,
						splitPoint, end,
					})
					end = splitPoint
				}
			}
		}

		bufSent := false
		buf = buf[:end-start]
		defer func() {
			if !bufSent {
				releaseBuf(oribuf, false)
				oribuf = nil
				buf = nil
			}
		}()

		fs.Debugf(src, "multi-thread copy: chunk finish: stream %d, chunk %d size %v err %v, took %v", stream+1, fs.SizeSuffix(start), fs.SizeSuffix(end-start), err, time.Now().Sub(startTime))
		if err != nil {
			_ = rc.Close()
			safeChanSend(errChan, err)
			return true
		}

		bufSent = true
		if !safeChanSend(resultChan, ResultChunk{buf, chunkI, start, end}) {
			bufSent = false
		}

		if err := rc.Close(); err != nil {
			safeChanSend(errChan, err)
			return true
		}
		return false
	}

	for i := 0; i < streams; i++ {
		go func(stream int) {
			defer func() {
				fs.Debugf(src, "streamDownloader %d finished..", stream)
			}()
			for {
				atomic.AddInt32(&waitingThread, 1)
				chunk := <-workQueue
				atomic.AddInt32(&waitingThread, -1)
				if stopped {
					break
				}
				start := chunk.start
				end := chunk.end
				chunkI := chunk.chunkI

				streamDownloadChunk(stream, start, end, chunkI)
			}
		}(i)
	}

	curChunk := 0
	// 4. taskPutter, drive streamDownloader to download as many as N out-of-order chunks
	//             i.e. download up to curChunk + N chunk
	taskPutter := func() {
		defer func() {
			fs.Debugf(src, "taskPutter finished..")
		}()
		i := 1
		for !stopped {
			curMaxChunk := curChunk + streams + ci.MultiThreadChunkAhead
			if curMaxChunk > totalChunks {
				curMaxChunk = totalChunks
			}
			if i < curMaxChunk {
				for ; i < curMaxChunk; i++ {
					// if we use curChunkChan, then don't blocks here :)
					safeChanSend(workQueue, chunkTasks[i])
				}
			} else if i == curMaxChunk {
				time.Sleep(50 * time.Millisecond)
			} else {
				panic("shouldn't be here")
			}
		}
	}
	go taskPutter()

	// 5. writerLoop, writes streamDownloader result to pipeReader
	//             bumps curChunk to sync with taskPutter
	writerLoop := func() {
		resultHeap := &chunkHeap{mu: &sync.Mutex{}}
		deheap.Init(resultHeap)
		defer func() {
			remainingChunk := resultHeap.chunks
			resultHeap.chunks = []ResultChunk{}
			fs.Debugf(src, "Releasing %d dangling chunk", len(remainingChunk))
			for _, chunk := range remainingChunk {
				releaseBuf(chunk.buf, false)
				chunk.buf = nil
			}
			multiThreadChunkPool.Flush()
			fs.Debugf(src, "writer finished..")
		}()

		defer fs.CheckClose(writer, &err)

		curChunkOff := int64(0)
		// Copy the data
		for {
			chunk := <-resultChan
			if chunk.buf == nil {
				break // chan closed
			}
			deheap.Push(resultHeap, chunk)
			if stopped {
				break
			}
			//fs.Debugf(src, "multi-thread copy: writeLoop: got chunk %d", chunk.index) // useless as this logic never errors
			for {
				hasCurChunk := false
				resultHeap.SyncCall(func() {
					//lastChunk := resultHeap.chunks[len(resultHeap.chunks)-1]
					if len(resultHeap.chunks) == 0 {
						hasCurChunk = false
						return
					}
					lastChunk := resultHeap.chunks[0]
					if lastChunk.start == curChunkOff {
						hasCurChunk = true
					}
				})
				if hasCurChunk {
					chunkContent := deheap.Pop(resultHeap).(ResultChunk)
					//if chunkContent.index != curChunk {
					if chunkContent.start != curChunkOff {
						panic(fmt.Sprintf("??1, chunk.index: %d, curChunk: %d, chunk.start: %v, curChunkOff: %v", chunkContent.index, curChunk, fs.SizeSuffix(chunkContent.start), fs.SizeSuffix(curChunkOff)))
					}
					startTime := time.Now()
					fs.Debugf(src, "multi-thread copy: writeLoop: writing chunk %d...", curChunk)
					if _, err := writer.Write(chunkContent.buf); err != nil {
						safeChanSend(errChan, err)
					}
					fs.Debugf(src, "multi-thread copy: writeLoop: wrote chunk %d to pipe, took %v", curChunk, time.Now().Sub(startTime))

					releaseBuf(chunkContent.buf, false)
					chunkContent.buf = nil

					if chunkTasks[curChunk].end == chunkContent.end {
						curChunk = chunkContent.index + 1
					} else {
						curChunk = chunkContent.index
					}
					curChunkOff = chunkContent.end
					//if curChunk == totalChunks {
					if chunkContent.end == totalSize {
						if curChunk != totalChunks {
							panic("????222")
						}
						stopped = true
						_ = writer.Close()
						// let f.Put call to cleanup other
					}
				} else {
					fs.Debugf(src, "multi-thread copy: writeLoop: waiting for chunk starts with %v", fs.SizeSuffix(curChunkOff))
					break
				}
			}
		}
	}
	go writerLoop()

	// 6. f.Put call goroutine, call f.Put using pipeReader as input
	//       we do in a separate goroutine, so we can still listen to errChan in the main goroutine
	var obj fs.Object
	go func() (err error) {
		defer func() {
			safeChanSend(errChan, err)
		}()
		in_ := tr.Account(ctx, reader).WithBuffer() // account and buffer the transfer
		var in io.Reader = in_

		var wrappedSrc fs.ObjectInfo = src
		// We try to pass the original object if possible
		if src.Remote() != remote {
			wrappedSrc = fs.NewOverrideRemote(src, remote)
		}

		hasHash := ci.CheckSum
		hashes := hash.NewHashSet(src.Fs().Hashes().GetOne()) // just pick one hash
		var hasher *hash.MultiHasher
		hasher, err = hash.NewMultiHasherTypes(hashes)
		if err != nil {
			fs.Debugf(src, "multi-thread hasher init failed, ignoring hash!")
			hasHash = false
		}

		if hasHash {
			in = io.TeeReader(in, hasher)
		}

		obj, err = f.Put(ctx, in, wrappedSrc)
		if err != nil {
			fs.Debugf(src, "multi-thread f.Put failed: %v!", err)
			return err
		}
		if hasHash {
			dst := object.NewStaticObjectInfo(obj.Remote(), obj.ModTime(ctx), obj.Size(), false, hasher.Sums(), src.Fs())
			if dst.Size() != src.Size() {
				return fmt.Errorf("multi-thread corrupted, size differs (%d vs %d)", dst.Size(), src.Size())
			}
			same, ht, _ := CheckHashes(ctx, dst, src)
			if !same {
				return fmt.Errorf("multi-thread corrupted, sum %v differs", ht)
			}
		}
		return nil
	}()

	// (Postponed first small chunk Read & Send)
	firstChunk, err := io.ReadAll(firstRc)
	safeChanSend(resultChan, ResultChunk{firstChunk, 0, 0, int64(len(firstChunk))})

	// 7. Main Logic: Monitor and Cleanup
	//      we have 4 goroutines (taskPutter -> streamDownloader -> writeLoop -> f.Put call), they should exit themselves in a short while
	//      1 pipe, we close it when writeLoop exits
	//      1 buffer pool, let gc do the job
	//      1 cancellable context, we defer close it
	//      many response body, defer closed
	//

	// check errors
	err = <-errChan
	if err == nil {
		fs.Debugf(src, "multi-thread copy success!")
	} else {
		fs.Debugf(src, "multi-thread copy failed: %v!", err)
	}

	// emit stop signal
	stopped = true
	go func() {
		time.Sleep(4 * time.Second)
		// cleanup everything
		cancel()
		time.Sleep(4 * time.Second)
		close(errChan)
		close(workQueue)
		close(resultChan)
	}()
	fs.Debugf(src, "multi-thread copy returned!")
	return obj, err
}
