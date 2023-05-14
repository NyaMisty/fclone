//go:build !plan9
// +build !plan9

// Package storj provides an interface to Storj decentralized object storage.
package storj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/fserrors"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/bucket"
	"golang.org/x/text/unicode/norm"

	"storj.io/uplink"
	"storj.io/uplink/edge"
)

const (
	existingProvider = "existing"
	newProvider      = "new"
)

var satMap = map[string]string{
	"us1.storj.io": "12EayRS2V1kEsWESU9QMRseFhdxYxKicsiFmxrsLZHeLUtdps3S@us1.storj.io:7777",
	"eu1.storj.io": "12L9ZFwhzVpuEKMUNUqkaTLGzwY9G24tbiigLiXpmZWKwmcNDDs@eu1.storj.io:7777",
	"ap1.storj.io": "121RTSDpyNZVcEU84Ticf2L1ntiuUimbWgfATz21tuvgk3vzoA6@ap1.storj.io:7777",
}

// Register with Fs
func init() {
	fs.Register(&fs.RegInfo{
		Name:        "storj",
		Description: "Storj Decentralized Cloud Storage",
		Aliases:     []string{"tardigrade"},
		NewFs:       NewFs,
		Config: func(ctx context.Context, name string, m configmap.Mapper, configIn fs.ConfigIn) (*fs.ConfigOut, error) {
			provider, _ := m.Get(fs.ConfigProvider)

			config.FileDeleteKey(name, fs.ConfigProvider)

			if provider == newProvider {
				satelliteString, _ := m.Get("satellite_address")
				apiKey, _ := m.Get("api_key")
				passphrase, _ := m.Get("passphrase")

				// satelliteString contains always default and passphrase can be empty
				if apiKey == "" {
					return nil, nil
				}

				satellite, found := satMap[satelliteString]
				if !found {
					satellite = satelliteString
				}

				access, err := uplink.RequestAccessWithPassphrase(context.TODO(), satellite, apiKey, passphrase)
				if err != nil {
					return nil, fmt.Errorf("couldn't create access grant: %w", err)
				}

				serializedAccess, err := access.Serialize()
				if err != nil {
					return nil, fmt.Errorf("couldn't serialize access grant: %w", err)
				}
				m.Set("satellite_address", satellite)
				m.Set("access_grant", serializedAccess)
			} else if provider == existingProvider {
				config.FileDeleteKey(name, "satellite_address")
				config.FileDeleteKey(name, "api_key")
				config.FileDeleteKey(name, "passphrase")
			} else {
				return nil, fmt.Errorf("invalid provider type: %s", provider)
			}
			return nil, nil
		},
		Options: []fs.Option{
			{
				Name:    fs.ConfigProvider,
				Help:    "Choose an authentication method.",
				Default: existingProvider,
				Examples: []fs.OptionExample{{
					Value: "existing",
					Help:  "Use an existing access grant.",
				}, {
					Value: newProvider,
					Help:  "Create a new access grant from satellite address, API key, and passphrase.",
				},
				}},
			{
				Name:     "access_grant",
				Help:     "Access grant.",
				Provider: "existing",
			},
			{
				Name:     "satellite_address",
				Help:     "Satellite address.\n\nCustom satellite address should match the format: `<nodeid>@<address>:<port>`.",
				Provider: newProvider,
				Default:  "us1.storj.io",
				Examples: []fs.OptionExample{{
					Value: "us1.storj.io",
					Help:  "US1",
				}, {
					Value: "eu1.storj.io",
					Help:  "EU1",
				}, {
					Value: "ap1.storj.io",
					Help:  "AP1",
				},
				},
			},
			{
				Name:     "api_key",
				Help:     "API key.",
				Provider: newProvider,
			},
			{
				Name:     "passphrase",
				Help:     "Encryption passphrase.\n\nTo access existing objects enter passphrase used for uploading.",
				Provider: newProvider,
			},
		},
	})
}

// Options defines the configuration for this backend
type Options struct {
	Access string `config:"access_grant"`

	SatelliteAddress string `config:"satellite_address"`
	APIKey           string `config:"api_key"`
	Passphrase       string `config:"passphrase"`
}

// Fs represents a remote to Storj
type Fs struct {
	name string // the name of the remote
	root string // root of the filesystem

	opts     Options      // parsed options
	features *fs.Features // optional features

	access *uplink.Access // parsed scope

	project *uplink.Project // project client
}

// Check the interfaces are satisfied.
var (
	_ fs.Fs           = &Fs{}
	_ fs.ListRer      = &Fs{}
	_ fs.PutStreamer  = &Fs{}
	_ fs.Mover        = &Fs{}
	_ fs.Copier       = &Fs{}
	_ fs.Purger       = &Fs{}
	_ fs.PublicLinker = &Fs{}
)

// NewFs creates a filesystem backed by Storj.
func NewFs(ctx context.Context, name, root string, m configmap.Mapper) (_ fs.Fs, err error) {
	// Setup filesystem and connection to Storj
	root = norm.NFC.String(root)
	root = strings.Trim(root, "/")

	f := &Fs{
		name: name,
		root: root,
	}

	// Parse config into Options struct
	err = configstruct.Set(m, &f.opts)
	if err != nil {
		return nil, err
	}

	// Parse access
	var access *uplink.Access

	if f.opts.Access != "" {
		access, err = uplink.ParseAccess(f.opts.Access)
		if err != nil {
			return nil, fmt.Errorf("storj: access: %w", err)
		}
	}

	if access == nil && f.opts.SatelliteAddress != "" && f.opts.APIKey != "" && f.opts.Passphrase != "" {
		access, err = uplink.RequestAccessWithPassphrase(ctx, f.opts.SatelliteAddress, f.opts.APIKey, f.opts.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("storj: access: %w", err)
		}

		serializedAccess, err := access.Serialize()
		if err != nil {
			return nil, fmt.Errorf("storj: access: %w", err)
		}

		err = config.SetValueAndSave(f.name, "access_grant", serializedAccess)
		if err != nil {
			return nil, fmt.Errorf("storj: access: %w", err)
		}
	}

	if access == nil {
		return nil, errors.New("access not found")
	}

	f.access = access

	f.features = (&fs.Features{
		BucketBased:       true,
		BucketBasedRootOK: true,
	}).Fill(ctx, f)

	project, err := f.connect(ctx)
	if err != nil {
		return nil, err
	}
	f.project = project

	// Root validation needs to check the following: If a bucket path is
	// specified and exists, then the object must be a directory.
	//
	// NOTE: At this point this must return the filesystem object we've
	// created so far even if there is an error.
	if root != "" {
		bucketName, bucketPath := bucket.Split(root)

		if bucketName != "" && bucketPath != "" {
			_, err = project.StatBucket(ctx, bucketName)
			if err != nil {
				return f, fmt.Errorf("storj: bucket: %w", err)
			}

			object, err := project.StatObject(ctx, bucketName, bucketPath)
			if err == nil {
				if !object.IsPrefix {
					// If the root is actually a file we
					// need to return the *parent*
					// directory of the root instead and an
					// error that the original root
					// requested is a file.
					newRoot := path.Dir(f.root)
					if newRoot == "." {
						newRoot = ""
					}
					f.root = newRoot

					return f, fs.ErrorIsFile
				}
			}
		}
	}

	return f, nil
}

// connect opens a connection to Storj.
func (f *Fs) connect(ctx context.Context) (project *uplink.Project, err error) {
	fs.Debugf(f, "connecting...")
	defer fs.Debugf(f, "connected: %+v", err)

	cfg := uplink.Config{
		UserAgent: "rclone",
	}

	project, err = cfg.OpenProject(ctx, f.access)
	if err != nil {
		return nil, fmt.Errorf("storj: project: %w", err)
	}

	return
}

// absolute computes the absolute bucket name and path from the filesystem root
// and the relative path provided.
func (f *Fs) absolute(relative string) (bucketName, bucketPath string) {
	bn, bp := bucket.Split(path.Join(f.root, relative))

	// NOTE: Technically libuplink does not care about the encoding. It is
	// happy to work with them as opaque byte sequences. However, rclone
	// has a test that requires two paths with the same normalized form
	// (but different un-normalized forms) to point to the same file. This
	// means we have to normalize before we interact with libuplink.
	return norm.NFC.String(bn), norm.NFC.String(bp)
}

// Name of the remote (as passed into NewFs)
func (f *Fs) Name() string {
	return f.name
}

// Root of the remote (as passed into NewFs)
func (f *Fs) Root() string {
	return f.root
}

// String returns a description of the FS
func (f *Fs) String() string {
	return fmt.Sprintf("FS sj://%s", f.root)
}

// Precision of the ModTimes in this Fs
func (f *Fs) Precision() time.Duration {
	return time.Nanosecond
}

// Hashes returns the supported hash types of the filesystem.
func (f *Fs) Hashes() hash.Set {
	return hash.NewHashSet()
}

// Features returns the optional features of this Fs
func (f *Fs) Features() *fs.Features {
	return f.features
}

// List the objects and directories in relative into entries. The entries can
// be returned in any order but should be for a complete directory.
//
// relative should be "" to list the root, and should not have trailing
// slashes.
//
// This should return fs.ErrDirNotFound if the directory isn't found.
func (f *Fs) List(ctx context.Context, relative string) (entries fs.DirEntries, err error) {
	fs.Debugf(f, "ls ./%s", relative)

	bucketName, bucketPath := f.absolute(relative)

	defer func() {
		if errors.Is(err, uplink.ErrBucketNotFound) {
			err = fs.ErrorDirNotFound
		}
	}()

	if bucketName == "" {
		if bucketPath != "" {
			return nil, fs.ErrorListBucketRequired
		}

		return f.listBuckets(ctx)
	}

	return f.listObjects(ctx, relative, bucketName, bucketPath)
}

func (f *Fs) listBuckets(ctx context.Context) (entries fs.DirEntries, err error) {
	fs.Debugf(f, "BKT ls")

	buckets := f.project.ListBuckets(ctx, nil)

	for buckets.Next() {
		bucket := buckets.Item()

		entries = append(entries, fs.NewDir(bucket.Name, bucket.Created))
	}

	return entries, buckets.Err()
}

// newDirEntry creates a directory entry from an uplink object.
//
// NOTE: Getting the exact behavior required by rclone is somewhat tricky. The
// path manipulation here is necessary to cover all the different ways the
// filesystem and object could be initialized and combined.
func (f *Fs) newDirEntry(relative, prefix string, object *uplink.Object) fs.DirEntry {
	if object.IsPrefix {
		//                         . The entry must include the relative path as its prefix. Depending on
		//                         | what is being listed and how the filesystem root was initialized the
		//                         | relative path may be empty (and so we use path joining here to ensure
		//                         | we don't end up with an empty path segment).
		//                         |
		//                         |                    . Remove the prefix used during listing.
		//                         |                    |
		//                         |                    |           . Remove the trailing slash.
		//                         |                    |           |
		//                         v                    v           v
		return fs.NewDir(path.Join(relative, object.Key[len(prefix):len(object.Key)-1]), object.System.Created)
	}

	return newObjectFromUplink(f, relative, object)
}

func (f *Fs) listObjects(ctx context.Context, relative, bucketName, bucketPath string) (entries fs.DirEntries, err error) {
	fs.Debugf(f, "OBJ ls ./%s (%q, %q)", relative, bucketName, bucketPath)

	opts := &uplink.ListObjectsOptions{
		Prefix: newPrefix(bucketPath),

		System: true,
		Custom: true,
	}
	fs.Debugf(f, "opts %+v", opts)

	objects := f.project.ListObjects(ctx, bucketName, opts)

	for objects.Next() {
		entries = append(entries, f.newDirEntry(relative, opts.Prefix, objects.Item()))
	}

	err = objects.Err()
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// ListR lists the objects and directories of the Fs starting from dir
// recursively into out.
//
// relative should be "" to start from the root, and should not have trailing
// slashes.
//
// This should return ErrDirNotFound if the directory isn't found.
//
// It should call callback for each tranche of entries read. These need not be
// returned in any particular order. If callback returns an error then the
// listing will stop immediately.
//
// Don't implement this unless you have a more efficient way of listing
// recursively that doing a directory traversal.
func (f *Fs) ListR(ctx context.Context, relative string, callback fs.ListRCallback) (err error) {
	fs.Debugf(f, "ls -R ./%s", relative)

	bucketName, bucketPath := f.absolute(relative)

	defer func() {
		if errors.Is(err, uplink.ErrBucketNotFound) {
			err = fs.ErrorDirNotFound
		}
	}()

	if bucketName == "" {
		if bucketPath != "" {
			return fs.ErrorListBucketRequired
		}

		return f.listBucketsR(ctx, callback)
	}

	return f.listObjectsR(ctx, relative, bucketName, bucketPath, callback)
}

func (f *Fs) listBucketsR(ctx context.Context, callback fs.ListRCallback) (err error) {
	fs.Debugf(f, "BKT ls -R")

	buckets := f.project.ListBuckets(ctx, nil)

	for buckets.Next() {
		bucket := buckets.Item()

		err = f.listObjectsR(ctx, bucket.Name, bucket.Name, "", callback)
		if err != nil {
			return err
		}
	}

	return buckets.Err()
}

func (f *Fs) listObjectsR(ctx context.Context, relative, bucketName, bucketPath string, callback fs.ListRCallback) (err error) {
	fs.Debugf(f, "OBJ ls -R ./%s (%q, %q)", relative, bucketName, bucketPath)

	opts := &uplink.ListObjectsOptions{
		Prefix:    newPrefix(bucketPath),
		Recursive: true,

		System: true,
		Custom: true,
	}

	objects := f.project.ListObjects(ctx, bucketName, opts)

	for objects.Next() {
		object := objects.Item()

		err = callback(fs.DirEntries{f.newDirEntry(relative, opts.Prefix, object)})
		if err != nil {
			return err
		}
	}

	err = objects.Err()
	if err != nil {
		return err
	}

	return nil
}

// NewObject finds the Object at relative. If it can't be found it returns the
// error ErrorObjectNotFound.
func (f *Fs) NewObject(ctx context.Context, relative string) (_ fs.Object, err error) {
	fs.Debugf(f, "stat ./%s", relative)

	bucketName, bucketPath := f.absolute(relative)

	object, err := f.project.StatObject(ctx, bucketName, bucketPath)
	if err != nil {
		fs.Debugf(f, "err: %+v", err)

		if errors.Is(err, uplink.ErrObjectNotFound) {
			return nil, fs.ErrorObjectNotFound
		}
		return nil, err
	}

	return newObjectFromUplink(f, relative, object), nil
}

// Put in to the remote path with the modTime given of the given size
//
// When called from outside an Fs by rclone, src.Size() will always be >= 0.
// But for unknown-sized objects (indicated by src.Size() == -1), Put should
// either return an error or upload it properly (rather than e.g. calling
// panic).
//
// May create the object even if it returns an error - if so will return the
// object and the error, otherwise will return nil and the error
func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (_ fs.Object, err error) {
	fs.Debugf(f, "cp input ./%s # %+v %d", src.Remote(), options, src.Size())

	// Reject options we don't support.
	for _, option := range options {
		if option.Mandatory() {
			fs.Errorf(f, "Unsupported mandatory option: %v", option)

			return nil, errors.New("unsupported mandatory option")
		}
	}

	bucketName, bucketPath := f.absolute(src.Remote())

	upload, err := f.project.UploadObject(ctx, bucketName, bucketPath, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			aerr := upload.Abort()
			if aerr != nil && !errors.Is(aerr, uplink.ErrUploadDone) {
				fs.Errorf(f, "cp input ./%s %+v: %+v", src.Remote(), options, aerr)
			}
		}
	}()

	err = upload.SetCustomMetadata(ctx, uplink.CustomMetadata{
		"rclone:mtime": src.ModTime(ctx).Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(upload, in)
	if err != nil {
		if errors.Is(err, uplink.ErrBucketNotFound) {
			// Rclone assumes the backend will create the bucket if not existing yet.
			// Here we create the bucket and return a retry error for rclone to retry the upload.
			_, err = f.project.EnsureBucket(ctx, bucketName)
			if err != nil {
				return nil, err
			}
			return nil, fserrors.RetryError(errors.New("bucket was not available, now created, the upload must be retried"))
		}

		err = fserrors.RetryError(err)
		fs.Errorf(f, "cp input ./%s %+v: %+v\n", src.Remote(), options, err)

		return nil, err
	}

	err = upload.Commit()
	if err != nil {
		if errors.Is(err, uplink.ErrBucketNotFound) {
			// Rclone assumes the backend will create the bucket if not existing yet.
			// Here we create the bucket and return a retry error for rclone to retry the upload.
			_, err = f.project.EnsureBucket(ctx, bucketName)
			if err != nil {
				return nil, err
			}
			err = fserrors.RetryError(errors.New("bucket was not available, now created, the upload must be retried"))
		}
		return nil, err
	}

	return newObjectFromUplink(f, src.Remote(), upload.Info()), nil
}

// PutStream uploads to the remote path with the modTime given of indeterminate
// size.
//
// May create the object even if it returns an error - if so will return the
// object and the error, otherwise will return nil and the error.
func (f *Fs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (_ fs.Object, err error) {
	return f.Put(ctx, in, src, options...)
}

// Mkdir makes the directory (container, bucket)
//
// Shouldn't return an error if it already exists
func (f *Fs) Mkdir(ctx context.Context, relative string) (err error) {
	fs.Debugf(f, "mkdir -p ./%s", relative)

	bucketName, _ := f.absolute(relative)

	_, err = f.project.EnsureBucket(ctx, bucketName)

	return err
}

// Rmdir removes the directory (container, bucket)
//
// NOTE: Despite code documentation to the contrary, this method should not
// return an error if the directory does not exist.
func (f *Fs) Rmdir(ctx context.Context, relative string) (err error) {
	fs.Debugf(f, "rmdir ./%s", relative)

	bucketName, bucketPath := f.absolute(relative)

	if bucketPath != "" {
		// If we can successfully stat it, then it is an object (and not a prefix).
		_, err := f.project.StatObject(ctx, bucketName, bucketPath)
		if err != nil {
			if errors.Is(err, uplink.ErrObjectNotFound) {
				// At this point we know it is not an object,
				// but we don't know if it is a prefix for one.
				//
				// We check this by doing a listing and if we
				// get any results back, then we know this is a
				// valid prefix (which implies the directory is
				// not empty).
				opts := &uplink.ListObjectsOptions{
					Prefix: newPrefix(bucketPath),

					System: true,
					Custom: true,
				}

				objects := f.project.ListObjects(ctx, bucketName, opts)

				if objects.Next() {
					return fs.ErrorDirectoryNotEmpty
				}

				return objects.Err()
			}

			return err
		}

		return fs.ErrorIsFile
	}

	_, err = f.project.DeleteBucket(ctx, bucketName)
	if err != nil {
		if errors.Is(err, uplink.ErrBucketNotFound) {
			return fs.ErrorDirNotFound
		}

		if errors.Is(err, uplink.ErrBucketNotEmpty) {
			return fs.ErrorDirectoryNotEmpty
		}

		return err
	}

	return nil
}

// newPrefix returns a new prefix for listing conforming to the libuplink
// requirements. In particular, libuplink requires a trailing slash for
// listings, but rclone does not always provide one. Further, depending on how
// the path was initially path normalization may have removed it (e.g. a
// trailing slash from the CLI is removed before it ever gets to the backend
// code).
func newPrefix(prefix string) string {
	if prefix == "" {
		return prefix
	}

	if prefix[len(prefix)-1] == '/' {
		return prefix
	}

	return prefix + "/"
}

// Move src to this remote using server-side move operations.
//
// This is stored with the remote path given.
//
// It returns the destination Object and a possible error.
//
// Will only be called if src.Fs().Name() == f.Name()
//
// If it isn't possible then return fs.ErrorCantMove
func (f *Fs) Move(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	srcObj, ok := src.(*Object)
	if !ok {
		fs.Debugf(src, "Can't move - not same remote type")
		return nil, fs.ErrorCantMove
	}

	// Move parameters
	srcBucket, srcKey := bucket.Split(srcObj.absolute)
	dstBucket, dstKey := f.absolute(remote)
	options := uplink.MoveObjectOptions{}

	// Do the move
	err := f.project.MoveObject(ctx, srcBucket, srcKey, dstBucket, dstKey, &options)
	if err != nil {
		// Make sure destination bucket exists
		_, err := f.project.EnsureBucket(ctx, dstBucket)
		if err != nil {
			return nil, fmt.Errorf("rename object failed to create destination bucket: %w", err)
		}
		// And try again
		err = f.project.MoveObject(ctx, srcBucket, srcKey, dstBucket, dstKey, &options)
		if err != nil {
			return nil, fmt.Errorf("rename object failed: %w", err)
		}
	}

	// Read the new object
	return f.NewObject(ctx, remote)
}

// Copy src to this remote using server-side copy operations.
//
// This is stored with the remote path given.
//
// It returns the destination Object and a possible error.
//
// Will only be called if src.Fs().Name() == f.Name()
//
// If it isn't possible then return fs.ErrorCantCopy
func (f *Fs) Copy(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	srcObj, ok := src.(*Object)
	if !ok {
		fs.Debugf(src, "Can't copy - not same remote type")
		return nil, fs.ErrorCantCopy
	}

	// Copy parameters
	srcBucket, srcKey := bucket.Split(srcObj.absolute)
	dstBucket, dstKey := f.absolute(remote)
	options := uplink.CopyObjectOptions{}

	// Do the copy
	newObject, err := f.project.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey, &options)
	if err != nil {
		// Make sure destination bucket exists
		_, err := f.project.EnsureBucket(ctx, dstBucket)
		if err != nil {
			return nil, fmt.Errorf("copy object failed to create destination bucket: %w", err)
		}
		// And try again
		newObject, err = f.project.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey, &options)
		if err != nil {
			return nil, fmt.Errorf("copy object failed: %w", err)
		}
	}

	// Return the new object
	return newObjectFromUplink(f, remote, newObject), nil
}

// Purge all files in the directory specified
//
// Implement this if you have a way of deleting all the files
// quicker than just running Remove() on the result of List()
//
// Return an error if it doesn't exist
func (f *Fs) Purge(ctx context.Context, dir string) error {
	bucket, directory := f.absolute(dir)
	if bucket == "" {
		return errors.New("can't purge from root")
	}

	if directory == "" {
		_, err := f.project.DeleteBucketWithObjects(ctx, bucket)
		if errors.Is(err, uplink.ErrBucketNotFound) {
			return fs.ErrorDirNotFound
		}
		return err
	}

	fs.Infof(directory, "Quick delete is available only for entire bucket. Falling back to list and delete.")
	objects := f.project.ListObjects(ctx, bucket,
		&uplink.ListObjectsOptions{
			Prefix:    directory + "/",
			Recursive: true,
		},
	)
	if err := objects.Err(); err != nil {
		return err
	}

	empty := true
	for objects.Next() {
		empty = false
		_, err := f.project.DeleteObject(ctx, bucket, objects.Item().Key)
		if err != nil {
			return err
		}
		fs.Infof(objects.Item().Key, "Deleted")
	}

	if empty {
		return fs.ErrorDirNotFound
	}

	return nil
}

// PublicLink generates a public link to the remote path (usually readable by anyone)
func (f *Fs) PublicLink(ctx context.Context, remote string, expire fs.Duration, unlink bool) (string, error) {
	bucket, key := f.absolute(remote)
	if bucket == "" {
		return "", errors.New("path must be specified")
	}

	// Rclone requires that a link is only generated if the remote path exists
	if key == "" {
		_, err := f.project.StatBucket(ctx, bucket)
		if err != nil {
			return "", err
		}
	} else {
		_, err := f.project.StatObject(ctx, bucket, key)
		if err != nil {
			if !errors.Is(err, uplink.ErrObjectNotFound) {
				return "", err
			}
			// No object found, check if there is such a prefix
			iter := f.project.ListObjects(ctx, bucket, &uplink.ListObjectsOptions{Prefix: key + "/"})
			if iter.Err() != nil {
				return "", iter.Err()
			}
			if !iter.Next() {
				return "", err
			}
		}
	}

	sharedPrefix := uplink.SharePrefix{Bucket: bucket, Prefix: key}

	permission := uplink.ReadOnlyPermission()
	if expire.IsSet() {
		permission.NotAfter = time.Now().Add(time.Duration(expire))
	}

	sharedAccess, err := f.access.Share(permission, sharedPrefix)
	if err != nil {
		return "", fmt.Errorf("sharing access to object failed: %w", err)
	}

	creds, err := (&edge.Config{
		AuthServiceAddress: "auth.storjshare.io:7777",
	}).RegisterAccess(ctx, sharedAccess, &edge.RegisterAccessOptions{Public: true})
	if err != nil {
		return "", fmt.Errorf("creating public link failed: %w", err)
	}

	return edge.JoinShareURL("https://link.storjshare.io", creds.AccessKeyID, bucket, key, nil)
}
