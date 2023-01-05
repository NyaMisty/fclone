package s3

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fstest"
	"github.com/rclone/rclone/fstest/fstests"
	"github.com/rclone/rclone/lib/random"
	"github.com/rclone/rclone/lib/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gz(t *testing.T, s string) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(s))
	require.NoError(t, err)
	err = zw.Close()
	require.NoError(t, err)
	return buf.String()
}

func md5sum(t *testing.T, s string) string {
	hash := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", hash)
}

func (f *Fs) InternalTestMetadata(t *testing.T) {
	ctx := context.Background()
	original := random.String(1000)
	contents := gz(t, original)

	item := fstest.NewItem("test-metadata", contents, fstest.Time("2001-05-06T04:05:06.499999999Z"))
	btime := time.Now()
	metadata := fs.Metadata{
		"cache-control":       "no-cache",
		"content-disposition": "inline",
		"content-encoding":    "gzip",
		"content-language":    "en-US",
		"content-type":        "text/plain",
		"mtime":               "2009-05-06T04:05:06.499999999Z",
		// "tier" - read only
		// "btime" - read only
	}
	obj := fstests.PutTestContentsMetadata(ctx, t, f, &item, contents, true, "text/html", metadata)
	defer func() {
		assert.NoError(t, obj.Remove(ctx))
	}()
	o := obj.(*Object)
	gotMetadata, err := o.Metadata(ctx)
	require.NoError(t, err)
	for k, v := range metadata {
		got := gotMetadata[k]
		switch k {
		case "mtime":
			assert.True(t, fstest.Time(v).Equal(fstest.Time(got)))
		case "btime":
			gotBtime := fstest.Time(got)
			dt := gotBtime.Sub(btime)
			assert.True(t, dt < time.Minute && dt > -time.Minute, fmt.Sprintf("btime more than 1 minute out want %v got %v delta %v", btime, gotBtime, dt))
			assert.True(t, fstest.Time(v).Equal(fstest.Time(got)))
		case "tier":
			assert.NotEqual(t, "", got)
		default:
			assert.Equal(t, v, got, k)
		}
	}

	t.Run("GzipEncoding", func(t *testing.T) {
		// Test that the gzipped file we uploaded can be
		// downloaded with and without decompression
		checkDownload := func(wantContents string, wantSize int64, wantHash string) {
			gotContents := fstests.ReadObject(ctx, t, o, -1)
			assert.Equal(t, wantContents, gotContents)
			assert.Equal(t, wantSize, o.Size())
			gotHash, err := o.Hash(ctx, hash.MD5)
			require.NoError(t, err)
			assert.Equal(t, wantHash, gotHash)
		}

		t.Run("NoDecompress", func(t *testing.T) {
			checkDownload(contents, int64(len(contents)), md5sum(t, contents))
		})
		t.Run("Decompress", func(t *testing.T) {
			f.opt.Decompress = true
			defer func() {
				f.opt.Decompress = false
			}()
			checkDownload(original, -1, "")
		})

	})
}

func (f *Fs) InternalTestNoHead(t *testing.T) {
	ctx := context.Background()
	// Set NoHead for this test
	f.opt.NoHead = true
	defer func() {
		f.opt.NoHead = false
	}()
	contents := random.String(1000)
	item := fstest.NewItem("test-no-head", contents, fstest.Time("2001-05-06T04:05:06.499999999Z"))
	obj := fstests.PutTestContents(ctx, t, f, &item, contents, true)
	defer func() {
		assert.NoError(t, obj.Remove(ctx))
	}()
	// PutTestcontents checks the received object

}

func TestVersionLess(t *testing.T) {
	key1 := "key1"
	key2 := "key2"
	t1 := fstest.Time("2022-01-21T12:00:00+01:00")
	t2 := fstest.Time("2022-01-21T12:00:01+01:00")
	for n, test := range []struct {
		a, b *s3.ObjectVersion
		want bool
	}{
		{a: nil, b: nil, want: true},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, b: nil, want: false},
		{a: nil, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, want: true},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, want: false},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t2}, want: false},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t2}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, want: true},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, b: &s3.ObjectVersion{Key: &key2, LastModified: &t1}, want: true},
		{a: &s3.ObjectVersion{Key: &key2, LastModified: &t1}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, want: false},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1, IsLatest: aws.Bool(false)}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, want: false},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1, IsLatest: aws.Bool(true)}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1}, want: true},
		{a: &s3.ObjectVersion{Key: &key1, LastModified: &t1, IsLatest: aws.Bool(false)}, b: &s3.ObjectVersion{Key: &key1, LastModified: &t1, IsLatest: aws.Bool(true)}, want: false},
	} {
		got := versionLess(test.a, test.b)
		assert.Equal(t, test.want, got, fmt.Sprintf("%d: %+v", n, test))
	}
}

func TestMergeDeleteMarkers(t *testing.T) {
	key1 := "key1"
	key2 := "key2"
	t1 := fstest.Time("2022-01-21T12:00:00+01:00")
	t2 := fstest.Time("2022-01-21T12:00:01+01:00")
	for n, test := range []struct {
		versions []*s3.ObjectVersion
		markers  []*s3.DeleteMarkerEntry
		want     []*s3.ObjectVersion
	}{
		{
			versions: []*s3.ObjectVersion{},
			markers:  []*s3.DeleteMarkerEntry{},
			want:     []*s3.ObjectVersion{},
		},
		{
			versions: []*s3.ObjectVersion{
				{
					Key:          &key1,
					LastModified: &t1,
				},
			},
			markers: []*s3.DeleteMarkerEntry{},
			want: []*s3.ObjectVersion{
				{
					Key:          &key1,
					LastModified: &t1,
				},
			},
		},
		{
			versions: []*s3.ObjectVersion{},
			markers: []*s3.DeleteMarkerEntry{
				{
					Key:          &key1,
					LastModified: &t1,
				},
			},
			want: []*s3.ObjectVersion{
				{
					Key:          &key1,
					LastModified: &t1,
					Size:         isDeleteMarker,
				},
			},
		},
		{
			versions: []*s3.ObjectVersion{
				{
					Key:          &key1,
					LastModified: &t2,
				},
				{
					Key:          &key2,
					LastModified: &t2,
				},
			},
			markers: []*s3.DeleteMarkerEntry{
				{
					Key:          &key1,
					LastModified: &t1,
				},
			},
			want: []*s3.ObjectVersion{
				{
					Key:          &key1,
					LastModified: &t2,
				},
				{
					Key:          &key1,
					LastModified: &t1,
					Size:         isDeleteMarker,
				},
				{
					Key:          &key2,
					LastModified: &t2,
				},
			},
		},
	} {
		got := mergeDeleteMarkers(test.versions, test.markers)
		assert.Equal(t, test.want, got, fmt.Sprintf("%d: %+v", n, test))
	}
}

func (f *Fs) InternalTestVersions(t *testing.T) {
	ctx := context.Background()

	// Enable versioning for this bucket during this test
	_, err := f.setGetVersioning(ctx, "Enabled")
	if err != nil {
		t.Skipf("Couldn't enable versioning: %v", err)
	}
	defer func() {
		// Disable versioning for this bucket
		_, err := f.setGetVersioning(ctx, "Suspended")
		assert.NoError(t, err)
	}()

	// Small pause to make the LastModified different since AWS
	// only seems to track them to 1 second granularity
	time.Sleep(2 * time.Second)

	// Create an object
	const fileName = "test-versions.txt"
	contents := random.String(100)
	item := fstest.NewItem(fileName, contents, fstest.Time("2001-05-06T04:05:06.499999999Z"))
	obj := fstests.PutTestContents(ctx, t, f, &item, contents, true)
	defer func() {
		assert.NoError(t, obj.Remove(ctx))
	}()

	// Small pause
	time.Sleep(2 * time.Second)

	// Remove it
	assert.NoError(t, obj.Remove(ctx))

	// Small pause to make the LastModified different since AWS only seems to track them to 1 second granularity
	time.Sleep(2 * time.Second)

	// And create it with different size and contents
	newContents := random.String(101)
	newItem := fstest.NewItem(fileName, newContents, fstest.Time("2002-05-06T04:05:06.499999999Z"))
	newObj := fstests.PutTestContents(ctx, t, f, &newItem, newContents, true)

	t.Run("Versions", func(t *testing.T) {
		// Set --s3-versions for this test
		f.opt.Versions = true
		defer func() {
			f.opt.Versions = false
		}()

		// Read the contents
		entries, err := f.List(ctx, "")
		require.NoError(t, err)
		tests := 0
		var fileNameVersion string
		for _, entry := range entries {
			remote := entry.Remote()
			if remote == fileName {
				t.Run("ReadCurrent", func(t *testing.T) {
					assert.Equal(t, newContents, fstests.ReadObject(ctx, t, entry.(fs.Object), -1))
				})
				tests++
			} else if versionTime, p := version.Remove(remote); !versionTime.IsZero() && p == fileName {
				t.Run("ReadVersion", func(t *testing.T) {
					assert.Equal(t, contents, fstests.ReadObject(ctx, t, entry.(fs.Object), -1))
				})
				assert.WithinDuration(t, obj.(*Object).lastModified, versionTime, time.Second, "object time must be with 1 second of version time")
				fileNameVersion = remote
				tests++
			}
		}
		assert.Equal(t, 2, tests, "object missing from listing")

		// Check we can read the object with a version suffix
		t.Run("NewObject", func(t *testing.T) {
			o, err := f.NewObject(ctx, fileNameVersion)
			require.NoError(t, err)
			require.NotNil(t, o)
			assert.Equal(t, int64(100), o.Size(), o.Remote())
		})
	})

	t.Run("VersionAt", func(t *testing.T) {
		// We set --s3-version-at for this test so make sure we reset it at the end
		defer func() {
			f.opt.VersionAt = fs.Time{}
		}()

		var (
			firstObjectTime  = obj.(*Object).lastModified
			secondObjectTime = newObj.(*Object).lastModified
		)

		for _, test := range []struct {
			what     string
			at       time.Time
			want     []fstest.Item
			wantErr  error
			wantSize int64
		}{
			{
				what:    "Before",
				at:      firstObjectTime.Add(-time.Second),
				want:    fstests.InternalTestFiles,
				wantErr: fs.ErrorObjectNotFound,
			},
			{
				what:     "AfterOne",
				at:       firstObjectTime.Add(time.Second),
				want:     append([]fstest.Item{item}, fstests.InternalTestFiles...),
				wantSize: 100,
			},
			{
				what:    "AfterDelete",
				at:      secondObjectTime.Add(-time.Second),
				want:    fstests.InternalTestFiles,
				wantErr: fs.ErrorObjectNotFound,
			},
			{
				what:     "AfterTwo",
				at:       secondObjectTime.Add(time.Second),
				want:     append([]fstest.Item{newItem}, fstests.InternalTestFiles...),
				wantSize: 101,
			},
		} {
			t.Run(test.what, func(t *testing.T) {
				f.opt.VersionAt = fs.Time(test.at)
				t.Run("List", func(t *testing.T) {
					fstest.CheckListing(t, f, test.want)
				})
				t.Run("NewObject", func(t *testing.T) {
					gotObj, gotErr := f.NewObject(ctx, fileName)
					assert.Equal(t, test.wantErr, gotErr)
					if gotErr == nil {
						assert.Equal(t, test.wantSize, gotObj.Size())
					}
				})
			})
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		require.NoError(t, f.CleanUpHidden(ctx))
		items := append([]fstest.Item{newItem}, fstests.InternalTestFiles...)
		fstest.CheckListing(t, f, items)
		// Set --s3-versions for this test
		f.opt.Versions = true
		defer func() {
			f.opt.Versions = false
		}()
		fstest.CheckListing(t, f, items)
	})

	// Purge gets tested later
}

func (f *Fs) InternalTest(t *testing.T) {
	t.Run("Metadata", f.InternalTestMetadata)
	t.Run("NoHead", f.InternalTestNoHead)
	t.Run("Versions", f.InternalTestVersions)
}

var _ fstests.InternalTester = (*Fs)(nil)
