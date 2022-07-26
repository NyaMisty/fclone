// Package cache implements the Fs cache
package cache

import (
	"context"
	"runtime"
	"sync"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/filter"
	"github.com/rclone/rclone/lib/cache"
)

var (
	once  sync.Once // creation
	c     *cache.Cache
	mu    sync.Mutex            // mutex to protect remap
	remap = map[string]string{} // map user supplied names to canonical names
)

// Create the cache just once
func createOnFirstUse() {
	once.Do(func() {
		ci := fs.GetConfig(context.Background())
		c = cache.New()
		c.SetExpireDuration(ci.FsCacheExpireDuration)
		c.SetExpireInterval(ci.FsCacheExpireInterval)
		c.SetFinalizer(func(value interface{}) {
			if s, ok := value.(fs.Shutdowner); ok {
				_ = fs.CountError(s.Shutdown(context.Background()))
			}
		})
	})
}

// Canonicalize looks up fsString in the mapping from user supplied
// names to canonical names and return the canonical form
func Canonicalize(fsString string) string {
	createOnFirstUse()
	mu.Lock()
	canonicalName, ok := remap[fsString]
	mu.Unlock()
	if !ok {
		return fsString
	}
	fs.Debugf(nil, "fs cache: switching user supplied name %q for canonical name %q", fsString, canonicalName)
	return canonicalName
}

// Put in a mapping from fsString => canonicalName if they are different
func addMapping(fsString, canonicalName string) {
	if canonicalName == fsString {
		return
	}
	mu.Lock()
	remap[fsString] = canonicalName
	mu.Unlock()
}

// GetFn gets an fs.Fs named fsString either from the cache or creates
// it afresh with the create function
func GetFn(ctx context.Context, fsString string, create func(ctx context.Context, fsString string) (fs.Fs, error)) (f fs.Fs, err error) {
	createOnFirstUse()
	canonicalFsString := Canonicalize(fsString)
	created := false
	value, err := c.Get(canonicalFsString, func(canonicalFsString string) (f interface{}, ok bool, err error) {
		f, err = create(ctx, fsString) // always create the backend with the original non-canonicalised string
		ok = err == nil || err == fs.ErrorIsFile
		created = ok
		return f, ok, err
	})
	if err != nil && err != fs.ErrorIsFile {
		return nil, err
	}
	f = value.(fs.Fs)
	// Check we stored the Fs at the canonical name
	if created {
		canonicalName := fs.ConfigString(f)
		if canonicalName != canonicalFsString {
			// Note that if err == fs.ErrorIsFile at this moment
			// then we can't rename the remote as it will have the
			// wrong error status, we need to add a new one.
			if err == nil {
				fs.Debugf(nil, "fs cache: renaming cache item %q to be canonical %q", canonicalFsString, canonicalName)
				value, found := c.Rename(canonicalFsString, canonicalName)
				if found {
					f = value.(fs.Fs)
				}
				addMapping(canonicalFsString, canonicalName)
			} else {
				fs.Debugf(nil, "fs cache: adding new entry for parent of %q, %q", canonicalFsString, canonicalName)
				Put(canonicalName, f)
			}
		}
	}
	return f, err
}

// Pin f into the cache until Unpin is called
func Pin(f fs.Fs) {
	createOnFirstUse()
	c.Pin(fs.ConfigString(f))
}

// PinUntilFinalized pins f into the cache until x is garbage collected
//
// This calls runtime.SetFinalizer on x so it shouldn't have a
// finalizer already.
func PinUntilFinalized(f fs.Fs, x interface{}) {
	Pin(f)
	runtime.SetFinalizer(x, func(_ interface{}) {
		Unpin(f)
	})

}

// Unpin f from the cache
func Unpin(f fs.Fs) {
	createOnFirstUse()
	c.Unpin(fs.ConfigString(f))
}

// Get gets an fs.Fs named fsString either from the cache or creates it afresh
func Get(ctx context.Context, fsString string) (f fs.Fs, err error) {
	// If we are making a long lived backend which lives longer
	// than this request, we want to disconnect it from the
	// current context and in particular any WithCancel contexts,
	// but we want to preserve the config embedded in the context.
	newCtx := context.Background()
	newCtx = fs.CopyConfig(newCtx, ctx)
	newCtx = filter.CopyConfig(newCtx, ctx)
	return GetFn(newCtx, fsString, fs.NewFs)
}

// GetArr gets []fs.Fs from []fsStrings either from the cache or creates it afresh
func GetArr(ctx context.Context, fsStrings []string) (f []fs.Fs, err error) {
	var fArr []fs.Fs
	for _, fsString := range fsStrings {
		f1, err1 := GetFn(ctx, fsString, fs.NewFs)
		if err1 != nil {
			return fArr, err1
		}
		fArr = append(fArr, f1)
	}
	return fArr, nil
}

// Put puts an fs.Fs named fsString into the cache
func Put(fsString string, f fs.Fs) {
	createOnFirstUse()
	canonicalName := fs.ConfigString(f)
	c.Put(canonicalName, f)
	addMapping(fsString, canonicalName)
}

// ClearConfig deletes all entries which were based on the config name passed in
//
// Returns number of entries deleted
func ClearConfig(name string) (deleted int) {
	createOnFirstUse()
	return c.DeletePrefix(name + ":")
}

// Clear removes everything from the cache
func Clear() {
	createOnFirstUse()
	c.Clear()
}

// Entries returns the number of entries in the cache
func Entries() int {
	createOnFirstUse()
	return c.Entries()
}
