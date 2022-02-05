package operations

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"golang.org/x/sync/errgroup"
)

const (
	multithreadChunkSize     = 64 << 10
	multithreadChunkSizeMask = multithreadChunkSize - 1
	multithreadBufferSize    = 32 * 1024
)

// Return a boolean as to whether we should use multi thread copy for
// this transfer
func doMultiThreadCopy(ctx context.Context, f fs.Fs, src fs.Object) bool {
	ci := fs.GetConfig(ctx)

	// Disable multi thread if...

	// ...it isn't configured
	if ci.MultiThreadStreams <= 1 {
		return false
	}
	// ...size of object is less than cutoff
	if src.Size() < int64(ci.MultiThreadCutoff) {
		return false
	}
	// ...source doesn't support it
	dstFeatures := f.Features()
	if dstFeatures.OpenWriterAt == nil {
		return false
	}
	// ...if --multi-thread-streams not in use and source and
	// destination are both local
	if !ci.MultiThreadSet && dstFeatures.IsLocal && src.Fs().Features().IsLocal {
		return false
	}
	return true
}

// state for a multi-thread copy
type multiThreadCopyState struct {
	ctx      context.Context
	partSize int64
	size     int64
	wc       fs.WriterAtCloser
	src      fs.Object
	acc      *accounting.Account
	streams  int
}

// Copy a single stream into place
func (mc *multiThreadCopyState) copyStream(ctx context.Context, stream int) (err error) {
	ci := fs.GetConfig(ctx)
	defer func() {
		if err != nil {
			fs.Debugf(mc.src, "multi-thread copy: stream %d/%d failed: %v", stream+1, mc.streams, err)
		}
	}()
	start := int64(stream) * mc.partSize
	if start >= mc.size {
		return nil
	}
	end := start + mc.partSize
	if end > mc.size {
		end = mc.size
	}

	fs.Debugf(mc.src, "multi-thread copy: stream %d/%d (%d-%d) size %v starting", stream+1, mc.streams, start, end, fs.SizeSuffix(end-start))

	rc, err := NewReOpen(ctx, mc.src, ci.LowLevelRetries, &fs.RangeOption{Start: start, End: end - 1})
	if err != nil {
		return fmt.Errorf("multipart copy: failed to open source: %w", err)
	}
	defer fs.CheckClose(rc, &err)

	// Copy the data
	buf := make([]byte, multithreadBufferSize)
	offset := start
	for {
		// Check if context cancelled and exit if so
		if mc.ctx.Err() != nil {
			return mc.ctx.Err()
		}
		nr, er := rc.Read(buf)
		if nr > 0 {
			err = mc.acc.AccountRead(nr)
			if err != nil {
				return fmt.Errorf("multipart copy: accounting failed: %w", err)
			}
			nw, ew := mc.wc.WriteAt(buf[0:nr], offset)
			if nw > 0 {
				offset += int64(nw)
			}
			if ew != nil {
				return fmt.Errorf("multipart copy: write failed: %w", ew)
			}
			if nr != nw {
				return fmt.Errorf("multipart copy: %w", io.ErrShortWrite)
			}
		}
		if er != nil {
			if er != io.EOF {
				return fmt.Errorf("multipart copy: read failed: %w", er)
			}
			break
		}
	}

	if offset != end {
		return fmt.Errorf("multipart copy: wrote %d bytes but expected to write %d", offset-start, end-start)
	}

	fs.Debugf(mc.src, "multi-thread copy: stream %d/%d (%d-%d) size %v finished", stream+1, mc.streams, start, end, fs.SizeSuffix(end-start))
	return nil
}

// Calculate the chunk sizes and updated number of streams
func (mc *multiThreadCopyState) calculateChunks() {
	partSize := mc.size / int64(mc.streams)
	// Round partition size up so partSize * streams >= size
	if (mc.size % int64(mc.streams)) != 0 {
		partSize++
	}
	// round partSize up to nearest multithreadChunkSize boundary
	mc.partSize = (partSize + multithreadChunkSizeMask) &^ multithreadChunkSizeMask
	// recalculate number of streams
	mc.streams = int(mc.size / mc.partSize)
	// round streams up so partSize * streams >= size
	if (mc.size % mc.partSize) != 0 {
		mc.streams++
	}
}

// Copy src to (f, remote) using streams download threads and the OpenWriterAt feature
func multiThreadCopy(ctx context.Context, f fs.Fs, remote string, src fs.Object, streams int, tr *accounting.Transfer) (newDst fs.Object, err error) {
	openWriterAt := f.Features().OpenWriterAt
	if openWriterAt == nil {
		return nil, errors.New("multi-thread copy: OpenWriterAt not supported")
	}
	if src.Size() < 0 {
		return nil, errors.New("multi-thread copy: can't copy unknown sized file")
	}
	if src.Size() == 0 {
		return nil, errors.New("multi-thread copy: can't copy zero sized file")
	}

	g, gCtx := errgroup.WithContext(ctx)
	mc := &multiThreadCopyState{
		ctx:     gCtx,
		size:    src.Size(),
		src:     src,
		streams: streams,
	}
	mc.calculateChunks()

	// Make accounting
	mc.acc = tr.Account(ctx, nil)

	// create write file handle
	mc.wc, err = openWriterAt(gCtx, remote, mc.size)
	if err != nil {
		return nil, fmt.Errorf("multipart copy: failed to open destination: %w", err)
	}

	fs.Debugf(src, "Starting multi-thread copy with %d parts of size %v", mc.streams, fs.SizeSuffix(mc.partSize))
	for stream := 0; stream < mc.streams; stream++ {
		stream := stream
		g.Go(func() (err error) {
			return mc.copyStream(gCtx, stream)
		})
	}
	err = g.Wait()
	closeErr := mc.wc.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, fmt.Errorf("multi-thread copy: failed to close object after copy: %w", closeErr)
	}

	obj, err := f.NewObject(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("multi-thread copy: failed to find object after copy: %w", err)
	}

	err = obj.SetModTime(ctx, src.ModTime(ctx))
	switch err {
	case nil, fs.ErrorCantSetModTime, fs.ErrorCantSetModTimeWithoutDelete:
	default:
		return nil, fmt.Errorf("multi-thread copy: failed to set modification time: %w", err)
	}

	fs.Debugf(src, "Finished multi-thread copy with %d parts of size %v", mc.streams, fs.SizeSuffix(mc.partSize))
	return obj, nil
}
