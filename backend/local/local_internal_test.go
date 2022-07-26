package local

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/fstest"
	"github.com/rclone/rclone/lib/file"
	"github.com/rclone/rclone/lib/readers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain drives the tests
func TestMain(m *testing.M) {
	fstest.TestMain(m)
}

// Test copy with source file that's updating
func TestUpdatingCheck(t *testing.T) {
	r := fstest.NewRun(t)
	defer r.Finalise()
	filePath := "sub dir/local test"
	r.WriteFile(filePath, "content", time.Now())

	fd, err := file.Open(path.Join(r.LocalName, filePath))
	if err != nil {
		t.Fatalf("failed opening file %q: %v", filePath, err)
	}
	defer func() {
		require.NoError(t, fd.Close())
	}()

	fi, err := fd.Stat()
	require.NoError(t, err)
	o := &Object{size: fi.Size(), modTime: fi.ModTime(), fs: &Fs{}}
	wrappedFd := readers.NewLimitedReadCloser(fd, -1)
	hash, err := hash.NewMultiHasherTypes(hash.Supported())
	require.NoError(t, err)
	in := localOpenFile{
		o:    o,
		in:   wrappedFd,
		hash: hash,
		fd:   fd,
	}

	buf := make([]byte, 1)
	_, err = in.Read(buf)
	require.NoError(t, err)

	r.WriteFile(filePath, "content updated", time.Now())
	_, err = in.Read(buf)
	require.Errorf(t, err, "can't copy - source file is being updated")

	// turn the checking off and try again
	in.o.fs.opt.NoCheckUpdated = true

	r.WriteFile(filePath, "content updated", time.Now())
	_, err = in.Read(buf)
	require.NoError(t, err)

}

func TestSymlink(t *testing.T) {
	ctx := context.Background()
	r := fstest.NewRun(t)
	defer r.Finalise()
	f := r.Flocal.(*Fs)
	dir := f.root

	// Write a file
	modTime1 := fstest.Time("2001-02-03T04:05:10.123123123Z")
	file1 := r.WriteFile("file.txt", "hello", modTime1)

	// Write a symlink
	modTime2 := fstest.Time("2002-02-03T04:05:10.123123123Z")
	symlinkPath := filepath.Join(dir, "symlink.txt")
	require.NoError(t, os.Symlink("file.txt", symlinkPath))
	require.NoError(t, lChtimes(symlinkPath, modTime2, modTime2))

	// Object viewed as symlink
	file2 := fstest.NewItem("symlink.txt"+linkSuffix, "file.txt", modTime2)

	// Object viewed as destination
	file2d := fstest.NewItem("symlink.txt", "hello", modTime1)

	// Check with no symlink flags
	r.CheckLocalItems(t, file1)
	r.CheckRemoteItems(t)

	// Set fs into "-L" mode
	f.opt.FollowSymlinks = true
	f.opt.TranslateSymlinks = false
	f.lstat = os.Stat

	r.CheckLocalItems(t, file1, file2d)
	r.CheckRemoteItems(t)

	// Set fs into "-l" mode
	f.opt.FollowSymlinks = false
	f.opt.TranslateSymlinks = true
	f.lstat = os.Lstat

	fstest.CheckListingWithPrecision(t, r.Flocal, []fstest.Item{file1, file2}, nil, fs.ModTimeNotSupported)
	if haveLChtimes {
		r.CheckLocalItems(t, file1, file2)
	}

	// Create a symlink
	modTime3 := fstest.Time("2002-03-03T04:05:10.123123123Z")
	file3 := r.WriteObjectTo(ctx, r.Flocal, "symlink2.txt"+linkSuffix, "file.txt", modTime3, false)
	fstest.CheckListingWithPrecision(t, r.Flocal, []fstest.Item{file1, file2, file3}, nil, fs.ModTimeNotSupported)
	if haveLChtimes {
		r.CheckLocalItems(t, file1, file2, file3)
	}

	// Check it got the correct contents
	symlinkPath = filepath.Join(dir, "symlink2.txt")
	fi, err := os.Lstat(symlinkPath)
	require.NoError(t, err)
	assert.False(t, fi.Mode().IsRegular())
	linkText, err := os.Readlink(symlinkPath)
	require.NoError(t, err)
	assert.Equal(t, "file.txt", linkText)

	// Check that NewObject gets the correct object
	o, err := r.Flocal.NewObject(ctx, "symlink2.txt"+linkSuffix)
	require.NoError(t, err)
	assert.Equal(t, "symlink2.txt"+linkSuffix, o.Remote())
	assert.Equal(t, int64(8), o.Size())

	// Check that NewObject doesn't see the non suffixed version
	_, err = r.Flocal.NewObject(ctx, "symlink2.txt")
	require.Equal(t, fs.ErrorObjectNotFound, err)

	// Check reading the object
	in, err := o.Open(ctx)
	require.NoError(t, err)
	contents, err := ioutil.ReadAll(in)
	require.NoError(t, err)
	require.Equal(t, "file.txt", string(contents))
	require.NoError(t, in.Close())

	// Check reading the object with range
	in, err = o.Open(ctx, &fs.RangeOption{Start: 2, End: 5})
	require.NoError(t, err)
	contents, err = ioutil.ReadAll(in)
	require.NoError(t, err)
	require.Equal(t, "file.txt"[2:5+1], string(contents))
	require.NoError(t, in.Close())
}

func TestSymlinkError(t *testing.T) {
	m := configmap.Simple{
		"links":      "true",
		"copy_links": "true",
	}
	_, err := NewFs(context.Background(), "local", "/", m)
	assert.Equal(t, errLinksAndCopyLinks, err)
}

// Test hashes on updating an object
func TestHashOnUpdate(t *testing.T) {
	ctx := context.Background()
	r := fstest.NewRun(t)
	defer r.Finalise()
	const filePath = "file.txt"
	when := time.Now()
	r.WriteFile(filePath, "content", when)
	f := r.Flocal.(*Fs)

	// Get the object
	o, err := f.NewObject(ctx, filePath)
	require.NoError(t, err)

	// Test the hash is as we expect
	md5, err := o.Hash(ctx, hash.MD5)
	require.NoError(t, err)
	assert.Equal(t, "9a0364b9e99bb480dd25e1f0284c8555", md5)

	// Reupload it with diferent contents but same size and timestamp
	var b = bytes.NewBufferString("CONTENT")
	src := object.NewStaticObjectInfo(filePath, when, int64(b.Len()), true, nil, f)
	err = o.Update(ctx, b, src)
	require.NoError(t, err)

	// Check the hash is as expected
	md5, err = o.Hash(ctx, hash.MD5)
	require.NoError(t, err)
	assert.Equal(t, "45685e95985e20822fb2538a522a5ccf", md5)
}

// Test hashes on deleting an object
func TestHashOnDelete(t *testing.T) {
	ctx := context.Background()
	r := fstest.NewRun(t)
	defer r.Finalise()
	const filePath = "file.txt"
	when := time.Now()
	r.WriteFile(filePath, "content", when)
	f := r.Flocal.(*Fs)

	// Get the object
	o, err := f.NewObject(ctx, filePath)
	require.NoError(t, err)

	// Test the hash is as we expect
	md5, err := o.Hash(ctx, hash.MD5)
	require.NoError(t, err)
	assert.Equal(t, "9a0364b9e99bb480dd25e1f0284c8555", md5)

	// Delete the object
	require.NoError(t, o.Remove(ctx))

	// Test the hash cache is empty
	require.Nil(t, o.(*Object).hashes)

	// Test the hash returns an error
	_, err = o.Hash(ctx, hash.MD5)
	require.Error(t, err)
}

func TestMetadata(t *testing.T) {
	ctx := context.Background()
	r := fstest.NewRun(t)
	defer r.Finalise()
	const filePath = "metafile.txt"
	when := time.Now()
	const dayLength = len("2001-01-01")
	whenRFC := when.Format(time.RFC3339Nano)
	r.WriteFile(filePath, "metadata file contents", when)
	f := r.Flocal.(*Fs)

	// Get the object
	obj, err := f.NewObject(ctx, filePath)
	require.NoError(t, err)
	o := obj.(*Object)

	features := f.Features()

	var hasXID, hasAtime, hasBtime bool
	switch runtime.GOOS {
	case "darwin", "freebsd", "netbsd", "linux":
		hasXID, hasAtime, hasBtime = true, true, true
	case "openbsd", "solaris":
		hasXID, hasAtime = true, true
	case "windows":
		hasAtime, hasBtime = true, true
	case "plan9", "js":
		// nada
	default:
		t.Errorf("No test cases for OS %q", runtime.GOOS)
	}

	assert.True(t, features.ReadMetadata)
	assert.True(t, features.WriteMetadata)
	assert.Equal(t, xattrSupported, features.UserMetadata)

	t.Run("Xattr", func(t *testing.T) {
		if !xattrSupported {
			t.Skip()
		}
		m, err := o.getXattr()
		require.NoError(t, err)
		assert.Nil(t, m)

		inM := fs.Metadata{
			"potato":  "chips",
			"cabbage": "soup",
		}
		err = o.setXattr(inM)
		require.NoError(t, err)

		m, err = o.getXattr()
		require.NoError(t, err)
		assert.NotNil(t, m)
		assert.Equal(t, inM, m)
	})

	checkTime := func(m fs.Metadata, key string, when time.Time) {
		mt, ok := o.parseMetadataTime(m, key)
		assert.True(t, ok)
		dt := mt.Sub(when)
		precision := time.Second
		assert.True(t, dt >= -precision && dt <= precision, fmt.Sprintf("%s: dt %v outside +/- precision %v", key, dt, precision))
	}

	checkInt := func(m fs.Metadata, key string, base int) int {
		value, ok := o.parseMetadataInt(m, key, base)
		assert.True(t, ok)
		return value
	}
	t.Run("Read", func(t *testing.T) {
		m, err := o.Metadata(ctx)
		require.NoError(t, err)
		assert.NotNil(t, m)

		// All OSes have these
		checkInt(m, "mode", 8)
		checkTime(m, "mtime", when)

		assert.Equal(t, len(whenRFC), len(m["mtime"]))
		assert.Equal(t, whenRFC[:dayLength], m["mtime"][:dayLength])

		if hasAtime {
			checkTime(m, "atime", when)
		}
		if hasBtime {
			checkTime(m, "btime", when)
		}
		if hasXID {
			checkInt(m, "uid", 10)
			checkInt(m, "gid", 10)
		}
	})

	t.Run("Write", func(t *testing.T) {
		newAtimeString := "2011-12-13T14:15:16.999999999Z"
		newAtime := fstest.Time(newAtimeString)
		newMtimeString := "2011-12-12T14:15:16.999999999Z"
		newMtime := fstest.Time(newMtimeString)
		newBtimeString := "2011-12-11T14:15:16.999999999Z"
		newBtime := fstest.Time(newBtimeString)
		newM := fs.Metadata{
			"mtime": newMtimeString,
			"atime": newAtimeString,
			"btime": newBtimeString,
			// Can't test uid, gid without being root
			"mode":   "0767",
			"potato": "wedges",
		}
		err := o.writeMetadata(newM)
		require.NoError(t, err)

		m, err := o.Metadata(ctx)
		require.NoError(t, err)
		assert.NotNil(t, m)

		mode := checkInt(m, "mode", 8)
		if runtime.GOOS != "windows" {
			assert.Equal(t, 0767, mode&0777, fmt.Sprintf("mode wrong - expecting 0767 got 0%o", mode&0777))
		}

		checkTime(m, "mtime", newMtime)
		if hasAtime {
			checkTime(m, "atime", newAtime)
		}
		if haveSetBTime {
			checkTime(m, "btime", newBtime)
		}
		if xattrSupported {
			assert.Equal(t, "wedges", m["potato"])
		}
	})

}
