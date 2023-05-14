package vfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
)

var writerMap sync.Map

type MessagedWriter struct {
	mu             sync.Mutex
	openCount      int
	Id             string
	innerWriter    io.WriteCloser
	curWriteOffset int64
	wroteBytes     int64
	messaged       bool
	hasWrote       bool
}

const (
	MSG_WRITE int32 = 1
	MSG_TRUNC int32 = 2
)

func GetMessagedWriter(fh *WriteFileHandle, writerFunc func() io.WriteCloser) *MessagedWriter {
	retWriter := &MessagedWriter{
		Id:          fh.file.Path(),
		innerWriter: nil,
		wroteBytes:  0,
		messaged:    fh.file.messagedWrite,
		hasWrote:    false,
	}
	tempWriter := retWriter
	tempWriter.mu.Lock()
	_retWriter, loaded := writerMap.LoadOrStore(fh.file.Path(), retWriter)
	retWriter = _retWriter.(*MessagedWriter)
	if !loaded {
		retWriter.innerWriter = writerFunc()
	}
	tempWriter.mu.Unlock()

	retWriter.Open()
	return retWriter
}

func (w *MessagedWriter) Open() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.openCount++
	fs.Debugf(w.Id, "file openCount++ to %d", w.openCount)
}
func (w *MessagedWriter) blockFinishTrailer() (err error) {
	// header: [8B-magic"RCLONEMP"][4B-REQTYPE(MSG_WRITE)][8B-off][8B-size]
	hdr := make([]byte, 8+4+8+8)
	buf := bytes.NewBuffer(hdr)
	buf.Reset()
	buf.WriteString("RCLONEMP")
	binary.Write(buf, binary.BigEndian, MSG_WRITE)
	binary.Write(buf, binary.BigEndian, w.curWriteOffset)
	binary.Write(buf, binary.BigEndian, w.wroteBytes)
	_, err = w.innerWriter.Write(buf.Bytes())
	return
}
func (w *MessagedWriter) truncateTrailer(off int64) (err error) {
	// header: [8B-magic"RCLONEMP"][4B-REQTYPE(MSG_TRUNC)][8B-off]
	hdr := make([]byte, 8+4+8)
	buf := bytes.NewBuffer(hdr)
	buf.Reset()
	buf.WriteString("RCLONEMP")
	binary.Write(buf, binary.BigEndian, MSG_TRUNC)
	binary.Write(buf, binary.BigEndian, off)
	_, err = w.innerWriter.Write(buf.Bytes())
	return
}

func (w *MessagedWriter) Truncate(off int64) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncateTrailer(off)
}

func (w *MessagedWriter) WriteAt(data []byte, off int64) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.hasWrote {
		err = w.blockFinishTrailer()
	} else if w.curWriteOffset != off {
		err = w.blockFinishTrailer()
		w.curWriteOffset = off
		w.wroteBytes = 0
	}
	if err != nil {
		return
	}
	w.hasWrote = true
	n, err = w.innerWriter.Write(data)
	w.wroteBytes += int64(n)
	w.curWriteOffset += int64(n)
	return
}

// Close closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and EOF.
func (w *MessagedWriter) Close() (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err = nil

	w.openCount--
	fs.Debugf(w.Id, "file openCount-- to %d", w.openCount)
	if w.openCount > 0 {
		return
	}

	waitTime := 180 * time.Second
	if w.hasWrote {
		waitTime = 0 * time.Second
	}

	destroyFunc := func() {
		fs.Infof(w.Id, "Going to late close writer due to openCount gets to %d", w.openCount)
		var err error
		if w.hasWrote {
			err = w.blockFinishTrailer()
			if err != nil {
				fs.Errorf(w.Id, "Failed to write finish trailer, err: %v", err)
			}
		}
		err = w.innerWriter.Close()
		if err != nil {
			fs.Errorf(w.Id, "Failed to close writer, err: %v", err)
		}
		writerMap.Delete(w.Id)
	}
	// handle qbittorrent downloader's pattern
	// qbt: 1. open with flags=O_RDWR|O_CREATE|0x40000 2. close 3. open again with O_RDWR
	if waitTime == 0 {
		destroyFunc()
	} else {
		time.AfterFunc(waitTime, func() {
			if w.openCount <= 0 {
				w.mu.Lock()
				defer w.mu.Unlock()
				destroyFunc()
			}
		})
	}

	return
}

// WriteFileHandle is an open for write handle on a File
type WriteFileHandle struct {
	baseHandle
	mu          sync.Mutex
	cond        sync.Cond // cond lock for out of sequence writes
	remote      string
	pipeWriter  *MessagedWriter
	o           fs.Object
	result      chan error
	file        *File
	offset      int64
	flags       int
	closed      bool // set if handle has been closed
	writeCalled bool // set the first time Write() is called
	opened      bool
	truncated   bool
}

// Check interfaces
var (
	_ io.Writer   = (*WriteFileHandle)(nil)
	_ io.WriterAt = (*WriteFileHandle)(nil)
	_ io.Closer   = (*WriteFileHandle)(nil)
	_ io.Seeker   = (*WriteFileHandle)(nil)
)

func newWriteFileHandle(d *Dir, f *File, remote string, flags int) (*WriteFileHandle, error) {
	fh := &WriteFileHandle{
		remote: remote,
		flags:  flags,
		result: make(chan error, 1),
		file:   f,
	}
	fh.cond = sync.Cond{L: &fh.mu}
	fh.file.addWriter(fh)
	return fh, nil
}

// returns whether it is OK to truncate the file
func (fh *WriteFileHandle) safeToTruncate() bool {
	return fh.truncated || fh.flags&os.O_TRUNC != 0 || !fh.file.exists()
}

// openPending opens the file if there is a pending open
//
// call with the lock held
func (fh *WriteFileHandle) openPending() (err error) {
	if fh.opened {
		return nil
	}
	if !fh.safeToTruncate() {
		fs.Errorf(fh.remote, "WriteFileHandle: Can't open for write without O_TRUNC on existing file without --vfs-cache-mode >= writes")
		return EPERM
	}
	writerFunc := func(pipeReader *io.PipeReader) {
		// NB Rcat deals with Stats.Transferring, etc.
		o, err := operations.Rcat(context.TODO(), fh.file.Fs(), fh.remote, pipeReader, time.Now(), nil)
		if err != nil {
			fs.Errorf(fh.remote, "WriteFileHandle.New Rcat failed: %v", err)
		}
		// Close the pipeReader so the pipeWriter fails with ErrClosedPipe
		_ = pipeReader.Close()
		fh.o = o
		fh.result <- err
	}
	fh.pipeWriter = GetMessagedWriter(fh, func() io.WriteCloser {
		pipeReader, pipeWriter := io.Pipe()
		go writerFunc(pipeReader)
		return pipeWriter
	})
	fh.file.setSize(0)
	fh.truncated = true
	fh.file.Dir().addObject(fh.file) // make sure the directory has this object in it now
	fh.opened = true
	return nil
}

// String converts it to printable
func (fh *WriteFileHandle) String() string {
	if fh == nil {
		return "<nil *WriteFileHandle>"
	}
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.file == nil {
		return "<nil *WriteFileHandle.file>"
	}
	return fh.file.String() + " (w)"
}

// Node returns the Node associated with this - satisfies Noder interface
func (fh *WriteFileHandle) Node() Node {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.file
}

// WriteAt writes len(p) bytes from p to the underlying data stream at offset
// off. It returns the number of bytes written from p (0 <= n <= len(p)) and
// any error encountered that caused the write to stop early. WriteAt must
// return a non-nil error if it returns n < len(p).
//
// If WriteAt is writing to a destination with a seek offset, WriteAt should
// not affect nor be affected by the underlying seek offset.
//
// Clients of WriteAt can execute parallel WriteAt calls on the same
// destination if the ranges do not overlap.
//
// Implementations must not retain p.
func (fh *WriteFileHandle) WriteAt(p []byte, off int64) (n int, err error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.writeAt(p, off)
}

// Implementation of WriteAt - call with lock held
func (fh *WriteFileHandle) writeAt(p []byte, off int64) (n int, err error) {
	// defer log.Trace(fh.remote, "len=%d off=%d", len(p), off)("n=%d, fh.off=%d, err=%v", &n, &fh.offset, &err)
	if fh.closed {
		fs.Errorf(fh.remote, "WriteFileHandle.Write: error: %v", EBADF)
		return 0, ECLOSED
	}
	if fh.offset != off {
		waitSequential("write", fh.remote, &fh.cond, fh.file.VFS().Opt.WriteWait, &fh.offset, off)
	}
	if fh.offset != off && !fh.file.messagedWrite {
		fs.Errorf(fh.remote, "WriteFileHandle.Write: can't seek in file without --vfs-cache-mode >= writes")
		return 0, ESPIPE
	}
	if err = fh.openPending(); err != nil {
		return 0, err
	}

	oriSize := fh.file.Size()
	newSize := oriSize
	n, err = fh.pipeWriter.WriteAt(p, off)
	newOff := off + int64(n)
	//fh.offset = newOff
	if newOff > oriSize {
		newSize = newOff
	}

	fh.writeCalled = true
	fh.file.setSize(newSize)
	if err != nil {
		fs.Errorf(fh.remote, "WriteFileHandle.Write error: %v", err)
		return 0, err
	}
	// fs.Debugf(fh.remote, "WriteFileHandle.Write OK (%d bytes written)", n)
	fh.cond.Broadcast() // wake everyone up waiting for an in-sequence read
	return n, nil
}

// Write writes len(p) bytes from p to the underlying data stream. It returns
// the number of bytes written from p (0 <= n <= len(p)) and any error
// encountered that caused the write to stop early. Write must return a non-nil
// error if it returns n < len(p). Write must not modify the slice data, even
// temporarily.
//
// Implementations must not retain p.
func (fh *WriteFileHandle) Write(p []byte) (n int, err error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	// Since we can't seek, just call WriteAt with the current offset
	n, err = fh.writeAt(p, fh.offset)
	fh.offset += int64(n)
	return
}

// WriteString a string to the file
func (fh *WriteFileHandle) WriteString(s string) (n int, err error) {
	return fh.Write([]byte(s))
}

// Offset returns the offset of the file pointer
func (fh *WriteFileHandle) Offset() (offset int64) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.offset
}

// close the file handle returning EBADF if it has been
// closed already.
//
// Must be called with fh.mu held
func (fh *WriteFileHandle) close() (err error) {
	if fh.closed {
		return ECLOSED
	}
	fh.closed = true
	// leave writer open until file is transferred
	defer func() {
		fh.file.delWriter(fh)
	}()
	// If file not opened and not safe to truncate then leave file intact
	if !fh.opened && !fh.safeToTruncate() {
		return nil
	}
	if err = fh.openPending(); err != nil {
		return err
	}
	writeCloseErr := fh.pipeWriter.Close()
	err = <-fh.result
	if err == nil {
		fh.file.setObject(fh.o)
		err = writeCloseErr
	} else {
		// Remove vfs file entry when no object is present
		if fh.file.getObject() == nil {
			_ = fh.file.Remove()
		}
	}
	return err
}

// Close closes the file
func (fh *WriteFileHandle) Close() error {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.close()
}

// Flush is called on each close() of a file descriptor. So if a
// filesystem wants to return write errors in close() and the file has
// cached dirty data, this is a good place to write back data and
// return any errors. Since many applications ignore close() errors
// this is not always useful.
//
// NOTE: The flush() method may be called more than once for each
// open(). This happens if more than one file descriptor refers to an
// opened file due to dup(), dup2() or fork() calls. It is not
// possible to determine if a flush is final, so each flush should be
// treated equally. Multiple write-flush sequences are relatively
// rare, so this shouldn't be a problem.
//
// Filesystems shouldn't assume that flush will always be called after
// some writes, or that if will be called at all.
func (fh *WriteFileHandle) Flush() error {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.closed {
		fs.Debugf(fh.remote, "WriteFileHandle.Flush nothing to do")
		return nil
	}
	// fs.Debugf(fh.remote, "WriteFileHandle.Flush")
	// If Write hasn't been called then ignore the Flush - Release
	// will pick it up
	if !fh.writeCalled {
		fs.Debugf(fh.remote, "WriteFileHandle.Flush unwritten handle, writing 0 bytes to avoid race conditions")
		_, err := fh.writeAt([]byte{}, fh.offset)
		return err
	}
	err := fh.close()
	if err != nil {
		fs.Errorf(fh.remote, "WriteFileHandle.Flush error: %v", err)
		//} else {
		// fs.Debugf(fh.remote, "WriteFileHandle.Flush OK")
	}
	return err
}

// Release is called when we are finished with the file handle
//
// It isn't called directly from userspace so the error is ignored by
// the kernel
func (fh *WriteFileHandle) Release() error {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.closed {
		fs.Debugf(fh.remote, "WriteFileHandle.Release nothing to do")
		return nil
	}
	fs.Debugf(fh.remote, "WriteFileHandle.Release closing")
	err := fh.close()
	if err != nil {
		fs.Errorf(fh.remote, "WriteFileHandle.Release error: %v", err)
		//} else {
		// fs.Debugf(fh.remote, "WriteFileHandle.Release OK")
	}
	return err
}

// Stat returns info about the file
func (fh *WriteFileHandle) Stat() (os.FileInfo, error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.file, nil
}

// Seek to new file position
func (fh *WriteFileHandle) Seek(offset int64, whence int) (ret int64, err error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.file.messagedWrite {
		if fh.closed {
			return 0, ECLOSED
		}
		if !fh.opened && offset == 0 && whence != 2 {
			return 0, nil
		}
		if err = fh.openPending(); err != nil {
			return ret, err
		}
		switch whence {
		case io.SeekStart:
			fh.offset = 0
		case io.SeekEnd:
			fh.offset = fh.file.Size()
		}
		fh.offset += offset
		// we don't check the offset - the next Read will
		return fh.offset, nil
	} else {
		return 0, ENOSYS
	}
}

// Truncate file to given size
func (fh *WriteFileHandle) Truncate(size int64) (err error) {
	// defer log.Trace(fh.remote, "size=%d", size)("err=%v", &err)
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.file.messagedWrite {
		err = fh.pipeWriter.Truncate(size)
		return
	}
	if size != fh.offset {
		fs.Errorf(fh.remote, "WriteFileHandle: Truncate: Can't change size without --vfs-cache-mode >= writes")
		return EPERM
	}
	// File is correct size
	if size == 0 {
		fh.truncated = true
	}
	return nil
}

// Read reads up to len(p) bytes into p.
func (fh *WriteFileHandle) Read(p []byte) (n int, err error) {
	fs.Errorf(fh.remote, "WriteFileHandle: Read: Can't read and write to file without --vfs-cache-mode >= minimal")
	return 0, EPERM
}

// ReadAt reads len(p) bytes into p starting at offset off in the
// underlying input source. It returns the number of bytes read (0 <=
// n <= len(p)) and any error encountered.
func (fh *WriteFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	fs.Errorf(fh.remote, "WriteFileHandle: ReadAt: Can't read and write to file without --vfs-cache-mode >= minimal")
	return 0, EPERM
}

// Sync commits the current contents of the file to stable storage. Typically,
// this means flushing the file system's in-memory copy of recently written
// data to disk.
func (fh *WriteFileHandle) Sync() error {
	return nil
}
