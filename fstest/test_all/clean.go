// Clean the left over test files

package main

import (
	"context"
	"fmt"
	"log"
	"regexp"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/list"
	"github.com/rclone/rclone/fs/operations"
)

// MatchTestRemote matches the remote names used for testing (copied
// from fstest/fstest.go so we don't have to import that and get all
// its flags)
var MatchTestRemote = regexp.MustCompile(`^rclone-test-[abcdefghijklmnopqrstuvwxyz0123456789]{24}(_segments)?$`)

// cleanFs runs a single clean fs for left over directories
func cleanFs(ctx context.Context, remote string, cleanup bool) error {
	f, err := fs.NewFs(context.Background(), remote)
	if err != nil {
		return err
	}
	var lastErr error
	if cleanup {
		log.Printf("%q - running cleanup", remote)
		err = operations.CleanUp(ctx, f)
		if err != nil {
			lastErr = err
			fs.Errorf(f, "Cleanup failed: %v", err)
		}
	}
	entries, err := list.DirSorted(ctx, f, true, "")
	if err != nil {
		return err
	}
	err = entries.ForDirError(func(dir fs.Directory) error {
		dirPath := dir.Remote()
		fullPath := remote + dirPath
		if MatchTestRemote.MatchString(dirPath) {
			if *dryRun {
				log.Printf("Not Purging %s - -dry-run", fullPath)
				return nil
			}
			log.Printf("Purging %s", fullPath)
			dir, err := fs.NewFs(context.Background(), fullPath)
			if err != nil {
				err = fmt.Errorf("NewFs failed: %w", err)
				lastErr = err
				fs.Errorf(fullPath, "%v", err)
				return nil
			}
			err = operations.Purge(ctx, dir, "")
			if err != nil {
				err = fmt.Errorf("Purge failed: %w", err)
				lastErr = err
				fs.Errorf(dir, "%v", err)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return lastErr
}

// cleanRemotes cleans the list of remotes passed in
func cleanRemotes(conf *Config) error {
	var lastError error
	for _, backend := range conf.Backends {
		remote := backend.Remote
		log.Printf("%q - Cleaning", remote)
		err := cleanFs(context.Background(), remote, backend.CleanUp)
		if err != nil {
			lastError = err
			log.Printf("Failed to purge %q: %v", remote, err)
		}
	}
	return lastError
}
