//go:build !plan9 && !solaris && !js
// +build !plan9,!solaris,!js

package oracleobjectstorage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
	"github.com/rclone/rclone/fs"
)

// ------------------------------------------------------------
// Command Interface Implementation
// ------------------------------------------------------------

const (
	operationRename        = "rename"
	operationListMultiPart = "list-multipart-uploads"
	operationCleanup       = "cleanup"
)

var commandHelp = []fs.CommandHelp{{
	Name:  operationRename,
	Short: "change the name of an object",
	Long: `This command can be used to rename a object.

Usage Examples:

    rclone backend rename oos:bucket relative-object-path-under-bucket object-new-name
`,
	Opts: nil,
}, {
	Name:  operationListMultiPart,
	Short: "List the unfinished multipart uploads",
	Long: `This command lists the unfinished multipart uploads in JSON format.

    rclone backend list-multipart-uploads oos:bucket/path/to/object

It returns a dictionary of buckets with values as lists of unfinished
multipart uploads.

You can call it with no bucket in which case it lists all bucket, with
a bucket or with a bucket and path.

    {
      "test-bucket": [
                {
                        "namespace": "test-namespace",
                        "bucket": "test-bucket",
                        "object": "600m.bin",
                        "uploadId": "51dd8114-52a4-b2f2-c42f-5291f05eb3c8",
                        "timeCreated": "2022-07-29T06:21:16.595Z",
                        "storageTier": "Standard"
                }
        ]
`,
}, {
	Name:  operationCleanup,
	Short: "Remove unfinished multipart uploads.",
	Long: `This command removes unfinished multipart uploads of age greater than
max-age which defaults to 24 hours.

Note that you can use --interactive/-i or --dry-run with this command to see what
it would do.

    rclone backend cleanup oos:bucket/path/to/object
    rclone backend cleanup -o max-age=7w oos:bucket/path/to/object

Durations are parsed as per the rest of rclone, 2h, 7d, 7w etc.
`,
	Opts: map[string]string{
		"max-age": "Max age of upload to delete",
	},
},
}

/*
Command the backend to run a named command

The command run is name
args may be used to read arguments from
opts may be used to read optional arguments from

The result should be capable of being JSON encoded
If it is a string or a []string it will be shown to the user
otherwise it will be JSON encoded and shown to the user like that
*/
func (f *Fs) Command(ctx context.Context, commandName string, args []string,
	opt map[string]string) (result interface{}, err error) {
	// fs.Debugf(f, "command %v, args: %v, opts:%v", commandName, args, opt)
	switch commandName {
	case operationRename:
		if len(args) < 2 {
			return nil, fmt.Errorf("path to object or its new name to rename is empty")
		}
		remote := args[0]
		newName := args[1]
		return f.rename(ctx, remote, newName)
	case operationListMultiPart:
		return f.listMultipartUploadsAll(ctx)
	case operationCleanup:
		maxAge := 24 * time.Hour
		if opt["max-age"] != "" {
			maxAge, err = fs.ParseDuration(opt["max-age"])
			if err != nil {
				return nil, fmt.Errorf("bad max-age: %w", err)
			}
		}
		return nil, f.cleanUp(ctx, maxAge)
	default:
		return nil, fs.ErrorCommandNotFound
	}
}

func (f *Fs) rename(ctx context.Context, remote, newName string) (interface{}, error) {
	if remote == "" {
		return nil, fmt.Errorf("path to object file cannot be empty")
	}
	if newName == "" {
		return nil, fmt.Errorf("the object's new name cannot be empty")
	}
	o := &Object{
		fs:     f,
		remote: remote,
	}
	bucketName, objectPath := o.split()
	err := o.readMetaData(ctx)
	if err != nil {
		fs.Errorf(f, "failed to read object:%v %v ", objectPath, err)
		if strings.HasPrefix(objectPath, bucketName) {
			fs.Errorf(f, "warn: ensure object path: %v is relative to bucket:%v and doesn't include the bucket name",
				objectPath, bucketName)
		}
		return nil, fs.ErrorNotAFile
	}
	details := objectstorage.RenameObjectDetails{
		SourceName: common.String(objectPath),
		NewName:    common.String(newName),
	}
	request := objectstorage.RenameObjectRequest{
		NamespaceName:       common.String(f.opt.Namespace),
		BucketName:          common.String(bucketName),
		RenameObjectDetails: details,
		OpcClientRequestId:  nil,
		RequestMetadata:     common.RequestMetadata{},
	}
	var response objectstorage.RenameObjectResponse
	err = f.pacer.Call(func() (bool, error) {
		response, err = f.srv.RenameObject(ctx, request)
		return shouldRetry(ctx, response.HTTPResponse(), err)
	})
	if err != nil {
		return nil, err
	}
	fs.Infof(f, "success: renamed object-path: %v to %v", objectPath, newName)
	return "renamed successfully", nil
}

func (f *Fs) listMultipartUploadsAll(ctx context.Context) (uploadsMap map[string][]*objectstorage.MultipartUpload,
	err error) {
	uploadsMap = make(map[string][]*objectstorage.MultipartUpload)
	bucket, directory := f.split("")
	if bucket != "" {
		uploads, err := f.listMultipartUploads(ctx, bucket, directory)
		if err != nil {
			return uploadsMap, err
		}
		uploadsMap[bucket] = uploads
		return uploadsMap, nil
	}
	entries, err := f.listBuckets(ctx)
	if err != nil {
		return uploadsMap, err
	}
	for _, entry := range entries {
		bucket := entry.Remote()
		uploads, listErr := f.listMultipartUploads(ctx, bucket, "")
		if listErr != nil {
			err = listErr
			fs.Errorf(f, "%v", err)
		}
		uploadsMap[bucket] = uploads
	}
	return uploadsMap, err
}

// listMultipartUploads lists all outstanding multipart uploads for (bucket, key)
//
// Note that rather lazily we treat key as a prefix, so it matches
// directories and objects. This could surprise the user if they ask
// for "dir" and it returns "dirKey"
func (f *Fs) listMultipartUploads(ctx context.Context, bucketName, directory string) (
	uploads []*objectstorage.MultipartUpload, err error) {

	uploads = []*objectstorage.MultipartUpload{}
	req := objectstorage.ListMultipartUploadsRequest{
		NamespaceName: common.String(f.opt.Namespace),
		BucketName:    common.String(bucketName),
	}

	var response objectstorage.ListMultipartUploadsResponse
	for {
		err = f.pacer.Call(func() (bool, error) {
			response, err = f.srv.ListMultipartUploads(ctx, req)
			return shouldRetry(ctx, response.HTTPResponse(), err)
		})
		if err != nil {
			// fs.Debugf(f, "failed to list multi part uploads %v", err)
			return uploads, err
		}
		for index, item := range response.Items {
			if directory != "" && item.Object != nil && !strings.HasPrefix(*item.Object, directory) {
				continue
			}
			uploads = append(uploads, &response.Items[index])
		}
		if response.OpcNextPage == nil {
			break
		}
		req.Page = response.OpcNextPage
	}
	return uploads, nil
}
