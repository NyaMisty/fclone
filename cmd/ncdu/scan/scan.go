// Package scan does concurrent scanning of an Fs building up a directory tree.
package scan

import (
	"context"
	"fmt"
	"path"
	"sync"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/walk"
)

// Dir represents a directory found in the remote
type Dir struct {
	parent            *Dir
	path              string
	mu                sync.Mutex
	count             int64
	size              int64
	entries           fs.DirEntries
	dirs              map[string]*Dir
	readError         error
	entriesHaveErrors bool
}

// Parent returns the directory above this one
func (d *Dir) Parent() *Dir {
	// no locking needed since these are write once in newDir()
	return d.parent
}

// Path returns the position of the dir in the filesystem
func (d *Dir) Path() string {
	// no locking needed since these are write once in newDir()
	return d.path
}

// make a new directory
func newDir(parent *Dir, dirPath string, entries fs.DirEntries, err error) *Dir {
	d := &Dir{
		parent:    parent,
		path:      dirPath,
		entries:   entries,
		dirs:      make(map[string]*Dir),
		readError: err,
	}
	// Count size in this dir
	for _, entry := range entries {
		if o, ok := entry.(fs.Object); ok {
			d.count++
			d.size += o.Size()
		}
	}
	// Set my directory entry in parent
	if parent != nil {
		parent.mu.Lock()
		leaf := path.Base(dirPath)
		d.parent.dirs[leaf] = d
		parent.mu.Unlock()
	}
	// Accumulate counts in parents
	for ; parent != nil; parent = parent.parent {
		parent.mu.Lock()
		parent.count += d.count
		parent.size += d.size
		if d.readError != nil {
			parent.entriesHaveErrors = true
		}
		parent.mu.Unlock()
	}
	return d
}

// Entries returns a copy of the entries in the directory
func (d *Dir) Entries() fs.DirEntries {
	return append(fs.DirEntries(nil), d.entries...)
}

// Remove removes the i-th entry from the
// in-memory representation of the remote directory
func (d *Dir) Remove(i int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.remove(i)
}

// removes the i-th entry from the
// in-memory representation of the remote directory
//
// Call with d.mu held
func (d *Dir) remove(i int) {
	size := d.entries[i].Size()
	count := int64(1)

	subDir, ok := d.getDir(i)
	if ok {
		size = subDir.size
		count = subDir.count
		delete(d.dirs, path.Base(subDir.path))
	}

	d.size -= size
	d.count -= count
	d.entries = append(d.entries[:i], d.entries[i+1:]...)

	dir := d
	// populate changed size and count to parent(s)
	for parent := d.parent; parent != nil; parent = parent.parent {
		parent.mu.Lock()
		parent.dirs[path.Base(dir.path)] = dir
		parent.size -= size
		parent.count -= count
		dir = parent
		parent.mu.Unlock()
	}
}

// gets the directory of the i-th entry
//
// returns nil if it is a file
// returns a flag as to whether is directory or not
//
// Call with d.mu held
func (d *Dir) getDir(i int) (subDir *Dir, isDir bool) {
	obj := d.entries[i]
	dir, ok := obj.(fs.Directory)
	if !ok {
		return nil, false
	}
	leaf := path.Base(dir.Remote())
	subDir = d.dirs[leaf]
	return subDir, true
}

// GetDir returns the Dir of the i-th entry
//
// returns nil if it is a file
// returns a flag as to whether is directory or not
func (d *Dir) GetDir(i int) (subDir *Dir, isDir bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getDir(i)
}

// Attr returns the size and count for the directory
func (d *Dir) Attr() (size int64, count int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.size, d.count
}

// AttrI returns the size, count and flags for the i-th directory entry
func (d *Dir) AttrI(i int) (size int64, count int64, isDir bool, readable bool, entriesHaveErrors bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	subDir, isDir := d.getDir(i)

	if !isDir {
		return d.entries[i].Size(), 0, false, true, d.entriesHaveErrors, d.readError
	}
	if subDir == nil {
		return 0, 0, true, false, false, nil
	}
	size, count = subDir.Attr()
	return size, count, true, true, subDir.entriesHaveErrors, subDir.readError
}

// Scan the Fs passed in, returning a root directory channel and an
// error channel
func Scan(ctx context.Context, f fs.Fs) (chan *Dir, chan error, chan struct{}) {
	ci := fs.GetConfig(ctx)
	root := make(chan *Dir, 1)
	errChan := make(chan error, 1)
	updated := make(chan struct{}, 1)
	go func() {
		parents := map[string]*Dir{}
		err := walk.Walk(ctx, f, "", false, ci.MaxDepth, func(dirPath string, entries fs.DirEntries, err error) error {
			var parent *Dir
			if dirPath != "" {
				parentPath := path.Dir(dirPath)
				if parentPath == "." {
					parentPath = ""
				}
				var ok bool
				parent, ok = parents[parentPath]
				if !ok {
					errChan <- fmt.Errorf("couldn't find parent for %q", dirPath)
				}
			}
			d := newDir(parent, dirPath, entries, err)
			parents[dirPath] = d
			if dirPath == "" {
				root <- d
			}
			// Mark updated
			select {
			case updated <- struct{}{}:
			default:
				break
			}
			return nil
		})
		if err != nil {
			errChan <- fmt.Errorf("ncdu listing failed: %w", err)
		}
		errChan <- nil
	}()
	return root, errChan, updated
}
