// Package march traverses two directories in lock step
package march

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/dirtree"
	"github.com/rclone/rclone/fs/filter"
	"github.com/rclone/rclone/fs/list"
	"github.com/rclone/rclone/fs/walk"
	"golang.org/x/text/unicode/norm"
)

// March holds the data used to traverse two Fs simultaneously,
// calling Callback for each match
type March struct {
	// parameters
	Ctx                    context.Context // context for background goroutines
	Fdst                   fs.Fs           // source Fs
	Fsrc                   fs.Fs           // dest Fs
	Dir                    string          // directory
	NoTraverse             bool            // don't traverse the destination
	SrcIncludeAll          bool            // don't include all files in the src
	DstIncludeAll          bool            // don't include all files in the destination
	Callback               Marcher         // object to call with results
	NoCheckDest            bool            // transfer all objects regardless without checking dst
	NoUnicodeNormalization bool            // don't normalize unicode characters in filenames
	// internal state
	srcListDir listDirFn // function to call to list a directory in the src
	dstListDir listDirFn // function to call to list a directory in the dst
	transforms []matchTransformFn
}

// Marcher is called on each match
type Marcher interface {
	// SrcOnly is called for a DirEntry found only in the source
	SrcOnly(src fs.DirEntry) (recurse bool)
	// DstOnly is called for a DirEntry found only in the destination
	DstOnly(dst fs.DirEntry) (recurse bool)
	// Match is called for a DirEntry found both in the source and destination
	Match(ctx context.Context, dst, src fs.DirEntry) (recurse bool)
}

// init sets up a march over opt.Fsrc, and opt.Fdst calling back callback for each match
// Note: this will flag filter-aware backends on the source side
func (m *March) init(ctx context.Context) {
	ci := fs.GetConfig(ctx)
	m.srcListDir = m.makeListDir(ctx, m.Fsrc, m.SrcIncludeAll)
	if !m.NoTraverse {
		m.dstListDir = m.makeListDir(ctx, m.Fdst, m.DstIncludeAll)
	}
	// Now create the matching transform
	// ..normalise the UTF8 first
	if !m.NoUnicodeNormalization {
		m.transforms = append(m.transforms, norm.NFC.String)
	}
	// ..if destination is caseInsensitive then make it lower case
	// case Insensitive | src | dst | lower case compare |
	//                  | No  | No  | No                 |
	//                  | Yes | No  | No                 |
	//                  | No  | Yes | Yes                |
	//                  | Yes | Yes | Yes                |
	if m.Fdst.Features().CaseInsensitive || ci.IgnoreCaseSync {
		m.transforms = append(m.transforms, strings.ToLower)
	}
}

// list a directory into entries, err
type listDirFn func(dir string) (entries fs.DirEntries, err error)

// makeListDir makes constructs a listing function for the given fs
// and includeAll flags for marching through the file system.
// Note: this will optionally flag filter-aware backends!
func (m *March) makeListDir(ctx context.Context, f fs.Fs, includeAll bool) listDirFn {
	ci := fs.GetConfig(ctx)
	fi := filter.GetConfig(ctx)
	if !(ci.UseListR && f.Features().ListR != nil) && // !--fast-list active and
		!(ci.NoTraverse && fi.HaveFilesFrom()) { // !(--files-from and --no-traverse)
		return func(dir string) (entries fs.DirEntries, err error) {
			dirCtx := filter.SetUseFilter(m.Ctx, !includeAll) // make filter-aware backends constrain List
			return list.DirSorted(dirCtx, f, includeAll, dir)
		}
	}

	// This returns a closure for use when --fast-list is active or for when
	// --files-from and --no-traverse is set
	var (
		mu      sync.Mutex
		started bool
		dirs    dirtree.DirTree
		dirsErr error
	)
	return func(dir string) (entries fs.DirEntries, err error) {
		mu.Lock()
		defer mu.Unlock()
		if !started {
			dirCtx := filter.SetUseFilter(m.Ctx, !includeAll) // make filter-aware backends constrain List
			dirs, dirsErr = walk.NewDirTree(dirCtx, f, m.Dir, includeAll, ci.MaxDepth)
			started = true
		}
		if dirsErr != nil {
			return nil, dirsErr
		}
		entries, ok := dirs[dir]
		if !ok {
			err = fs.ErrorDirNotFound
		} else {
			delete(dirs, dir)
		}
		return entries, err
	}
}

// listDirJob describe a directory listing that needs to be done
type listDirJob struct {
	srcRemote string
	dstRemote string
	srcDepth  int
	dstDepth  int
	noSrc     bool
	noDst     bool
}

// Run starts the matching process off
func (m *March) Run(ctx context.Context) error {
	ci := fs.GetConfig(ctx)
	fi := filter.GetConfig(ctx)
	m.init(ctx)

	srcDepth := ci.MaxDepth
	if srcDepth < 0 {
		srcDepth = fs.MaxLevel
	}
	dstDepth := srcDepth
	if fi.Opt.DeleteExcluded {
		dstDepth = fs.MaxLevel
	}

	var mu sync.Mutex // Protects vars below
	var jobError error
	var errCount int

	// Start some directory listing go routines
	var wg sync.WaitGroup         // sync closing of go routines
	var traversing sync.WaitGroup // running directory traversals
	checkers := ci.Checkers
	in := make(chan listDirJob, checkers)
	for i := 0; i < checkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-m.Ctx.Done():
					return
				case job, ok := <-in:
					if !ok {
						return
					}
					jobs, err := m.processJob(job)
					if err != nil {
						mu.Lock()
						// Keep reference only to the first encountered error
						if jobError == nil {
							jobError = err
						}
						errCount++
						mu.Unlock()
					}
					if len(jobs) > 0 {
						traversing.Add(len(jobs))
						go func() {
							// Now we have traversed this directory, send these
							// jobs off for traversal in the background
							for _, newJob := range jobs {
								select {
								case <-m.Ctx.Done():
									// discard job if finishing
									traversing.Done()
								case in <- newJob:
								}
							}
						}()
					}
					traversing.Done()
				}
			}
		}()
	}

	// Start the process
	traversing.Add(1)
	in <- listDirJob{
		srcRemote: m.Dir,
		srcDepth:  srcDepth - 1,
		dstRemote: m.Dir,
		dstDepth:  dstDepth - 1,
		noDst:     m.NoCheckDest,
	}
	go func() {
		// when the context is cancelled discard the remaining jobs
		<-m.Ctx.Done()
		for range in {
			traversing.Done()
		}
	}()
	traversing.Wait()
	close(in)
	wg.Wait()

	if errCount > 1 {
		return fmt.Errorf("march failed with %d error(s): first error: %w", errCount, jobError)
	}
	return jobError
}

// Check to see if the context has been cancelled
func (m *March) aborting() bool {
	select {
	case <-m.Ctx.Done():
		return true
	default:
	}
	return false
}

// matchEntry is an entry plus transformed name
type matchEntry struct {
	entry fs.DirEntry
	leaf  string
	name  string
}

// matchEntries contains many matchEntry~s
type matchEntries []matchEntry

// Len is part of sort.Interface.
func (es matchEntries) Len() int { return len(es) }

// Swap is part of sort.Interface.
func (es matchEntries) Swap(i, j int) { es[i], es[j] = es[j], es[i] }

// Less is part of sort.Interface.
//
// Compare in order (name, leaf, remote)
func (es matchEntries) Less(i, j int) bool {
	ei, ej := &es[i], &es[j]
	if ei.name == ej.name {
		if ei.leaf == ej.leaf {
			return fs.CompareDirEntries(ei.entry, ej.entry) < 0
		}
		return ei.leaf < ej.leaf
	}
	return ei.name < ej.name
}

// Sort the directory entries by (name, leaf, remote)
//
// We use a stable sort here just in case there are
// duplicates. Assuming the remote delivers the entries in a
// consistent order, this will give the best user experience
// in syncing as it will use the first entry for the sync
// comparison.
func (es matchEntries) sort() {
	sort.Stable(es)
}

// make a matchEntries from a newMatch entries
func newMatchEntries(entries fs.DirEntries, transforms []matchTransformFn) matchEntries {
	es := make(matchEntries, len(entries))
	for i := range es {
		es[i].entry = entries[i]
		name := path.Base(entries[i].Remote())
		es[i].leaf = name
		for _, transform := range transforms {
			name = transform(name)
		}
		es[i].name = name
	}
	es.sort()
	return es
}

// matchPair is a matched pair of direntries returned by matchListings
type matchPair struct {
	src, dst fs.DirEntry
}

// matchTransformFn converts a name into a form which is used for
// comparison in matchListings.
type matchTransformFn func(name string) string

// Process the two listings, matching up the items in the two slices
// using the transform function on each name first.
//
// Into srcOnly go Entries which only exist in the srcList
// Into dstOnly go Entries which only exist in the dstList
// Into matches go matchPair's of src and dst which have the same name
//
// This checks for duplicates and checks the list is sorted.
func matchListings(srcListEntries, dstListEntries fs.DirEntries, transforms []matchTransformFn) (srcOnly fs.DirEntries, dstOnly fs.DirEntries, matches []matchPair) {
	srcList := newMatchEntries(srcListEntries, transforms)
	dstList := newMatchEntries(dstListEntries, transforms)

	for iSrc, iDst := 0, 0; ; iSrc, iDst = iSrc+1, iDst+1 {
		var src, dst fs.DirEntry
		var srcName, dstName string
		if iSrc < len(srcList) {
			src = srcList[iSrc].entry
			srcName = srcList[iSrc].name
		}
		if iDst < len(dstList) {
			dst = dstList[iDst].entry
			dstName = dstList[iDst].name
		}
		if src == nil && dst == nil {
			break
		}
		if src != nil && iSrc > 0 {
			prev := srcList[iSrc-1].entry
			prevName := srcList[iSrc-1].name
			if srcName == prevName && fs.DirEntryType(prev) == fs.DirEntryType(src) {
				fs.Logf(src, "Duplicate %s found in source - ignoring", fs.DirEntryType(src))
				iDst-- // ignore the src and retry the dst
				continue
			} else if srcName < prevName {
				// this should never happen since we sort the listings
				panic("Out of order listing in source")
			}
		}
		if dst != nil && iDst > 0 {
			prev := dstList[iDst-1].entry
			prevName := dstList[iDst-1].name
			if dstName == prevName && fs.DirEntryType(dst) == fs.DirEntryType(prev) {
				fs.Logf(dst, "Duplicate %s found in destination - ignoring", fs.DirEntryType(dst))
				iSrc-- // ignore the dst and retry the src
				continue
			} else if dstName < prevName {
				// this should never happen since we sort the listings
				panic("Out of order listing in destination")
			}
		}
		if src != nil && dst != nil {
			// we can't use CompareDirEntries because srcName, dstName could
			// be different then src.Remote() or dst.Remote()
			srcType := fs.DirEntryType(src)
			dstType := fs.DirEntryType(dst)
			if srcName > dstName || (srcName == dstName && srcType > dstType) {
				src = nil
				iSrc--
			} else if srcName < dstName || (srcName == dstName && srcType < dstType) {
				dst = nil
				iDst--
			}
		}
		// Debugf(nil, "src = %v, dst = %v", src, dst)
		switch {
		case src == nil && dst == nil:
			// do nothing
		case src == nil:
			dstOnly = append(dstOnly, dst)
		case dst == nil:
			srcOnly = append(srcOnly, src)
		default:
			matches = append(matches, matchPair{src: src, dst: dst})
		}
	}
	return
}

// processJob processes a listDirJob listing the source and
// destination directories, comparing them and returning a slice of
// more jobs
//
// returns errors using processError
func (m *March) processJob(job listDirJob) ([]listDirJob, error) {
	var (
		jobs                   []listDirJob
		srcList, dstList       fs.DirEntries
		srcListErr, dstListErr error
		wg                     sync.WaitGroup
		mu                     sync.Mutex
	)

	// List the src and dst directories
	if !job.noSrc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srcList, srcListErr = m.srcListDir(job.srcRemote)
		}()
	}
	if !m.NoTraverse && !job.noDst {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dstList, dstListErr = m.dstListDir(job.dstRemote)
		}()
	}

	// Wait for listings to complete and report errors
	wg.Wait()
	if srcListErr != nil {
		if job.srcRemote != "" {
			fs.Errorf(job.srcRemote, "error reading source directory: %v", srcListErr)
		} else {
			fs.Errorf(m.Fsrc, "error reading source root directory: %v", srcListErr)
		}
		srcListErr = fs.CountError(srcListErr)
		return nil, srcListErr
	}
	if dstListErr == fs.ErrorDirNotFound {
		// Copy the stuff anyway
	} else if dstListErr != nil {
		if job.dstRemote != "" {
			fs.Errorf(job.dstRemote, "error reading destination directory: %v", dstListErr)
		} else {
			fs.Errorf(m.Fdst, "error reading destination root directory: %v", dstListErr)
		}
		dstListErr = fs.CountError(dstListErr)
		return nil, dstListErr
	}

	// If NoTraverse is set, then try to find a matching object
	// for each item in the srcList to head dst object
	ci := fs.GetConfig(m.Ctx)
	limiter := make(chan struct{}, ci.Checkers)
	if m.NoTraverse && !m.NoCheckDest {
		for _, src := range srcList {
			wg.Add(1)
			limiter <- struct{}{}
			go func(limiter chan struct{}, src fs.DirEntry) {
				defer wg.Done()
				if srcObj, ok := src.(fs.Object); ok {
					leaf := path.Base(srcObj.Remote())
					dstObj, err := m.Fdst.NewObject(m.Ctx, path.Join(job.dstRemote, leaf))
					if err == nil {
						mu.Lock()
						dstList = append(dstList, dstObj)
						mu.Unlock()
					}
				}
				<-limiter
			}(limiter, src)
		}
		wg.Wait()
	}

	// Work out what to do and do it
	srcOnly, dstOnly, matches := matchListings(srcList, dstList, m.transforms)
	for _, src := range srcOnly {
		if m.aborting() {
			return nil, m.Ctx.Err()
		}
		recurse := m.Callback.SrcOnly(src)
		if recurse && job.srcDepth > 0 {
			jobs = append(jobs, listDirJob{
				srcRemote: src.Remote(),
				dstRemote: src.Remote(),
				srcDepth:  job.srcDepth - 1,
				noDst:     true,
			})
		}

	}
	for _, dst := range dstOnly {
		if m.aborting() {
			return nil, m.Ctx.Err()
		}
		recurse := m.Callback.DstOnly(dst)
		if recurse && job.dstDepth > 0 {
			jobs = append(jobs, listDirJob{
				srcRemote: dst.Remote(),
				dstRemote: dst.Remote(),
				dstDepth:  job.dstDepth - 1,
				noSrc:     true,
			})
		}
	}
	for _, match := range matches {
		if m.aborting() {
			return nil, m.Ctx.Err()
		}
		recurse := m.Callback.Match(m.Ctx, match.dst, match.src)
		if recurse && job.srcDepth > 0 && job.dstDepth > 0 {
			jobs = append(jobs, listDirJob{
				srcRemote: match.src.Remote(),
				dstRemote: match.dst.Remote(),
				srcDepth:  job.srcDepth - 1,
				dstDepth:  job.dstDepth - 1,
			})
		}
	}
	return jobs, nil
}
