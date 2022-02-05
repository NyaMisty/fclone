// Package filter controls the filtering of files
package filter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"golang.org/x/sync/errgroup"
)

// This is the globally active filter
//
// This is accessed through GetConfig and AddConfig
var globalConfig = mustNewFilter(nil)

// rule is one filter rule
type rule struct {
	Include bool
	Regexp  *regexp.Regexp
}

// Match returns true if rule matches path
func (r *rule) Match(path string) bool {
	return r.Regexp.MatchString(path)
}

// String the rule
func (r *rule) String() string {
	c := "-"
	if r.Include {
		c = "+"
	}
	return fmt.Sprintf("%s %s", c, r.Regexp.String())
}

// rules is a slice of rules
type rules struct {
	rules    []rule
	existing map[string]struct{}
}

// add adds a rule if it doesn't exist already
func (rs *rules) add(Include bool, re *regexp.Regexp) {
	if rs.existing == nil {
		rs.existing = make(map[string]struct{})
	}
	newRule := rule{
		Include: Include,
		Regexp:  re,
	}
	newRuleString := newRule.String()
	if _, ok := rs.existing[newRuleString]; ok {
		return // rule already exists
	}
	rs.rules = append(rs.rules, newRule)
	rs.existing[newRuleString] = struct{}{}
}

// clear clears all the rules
func (rs *rules) clear() {
	rs.rules = nil
	rs.existing = nil
}

// len returns the number of rules
func (rs *rules) len() int {
	return len(rs.rules)
}

// FilesMap describes the map of files to transfer
type FilesMap map[string]struct{}

// Opt configures the filter
type Opt struct {
	DeleteExcluded bool
	FilterRule     []string
	FilterFrom     []string
	ExcludeRule    []string
	ExcludeFrom    []string
	ExcludeFile    string
	IncludeRule    []string
	IncludeFrom    []string
	FilesFrom      []string
	FilesFromRaw   []string
	MinAge         fs.Duration
	MaxAge         fs.Duration
	MinSize        fs.SizeSuffix
	MaxSize        fs.SizeSuffix
	IgnoreCase     bool
}

// DefaultOpt is the default config for the filter
var DefaultOpt = Opt{
	MinAge:  fs.DurationOff,
	MaxAge:  fs.DurationOff,
	MinSize: fs.SizeSuffix(-1),
	MaxSize: fs.SizeSuffix(-1),
}

// Filter describes any filtering in operation
type Filter struct {
	Opt         Opt
	ModTimeFrom time.Time
	ModTimeTo   time.Time
	fileRules   rules
	dirRules    rules
	files       FilesMap // files if filesFrom
	dirs        FilesMap // dirs from filesFrom
}

// NewFilter parses the command line options and creates a Filter
// object.  If opt is nil, then DefaultOpt will be used
func NewFilter(opt *Opt) (f *Filter, err error) {
	f = &Filter{}

	// Make a copy of the options
	if opt != nil {
		f.Opt = *opt
	} else {
		f.Opt = DefaultOpt
	}

	// Filter flags
	if f.Opt.MinAge.IsSet() {
		f.ModTimeTo = time.Now().Add(-time.Duration(f.Opt.MinAge))
		fs.Debugf(nil, "--min-age %v to %v", f.Opt.MinAge, f.ModTimeTo)
	}
	if f.Opt.MaxAge.IsSet() {
		f.ModTimeFrom = time.Now().Add(-time.Duration(f.Opt.MaxAge))
		if !f.ModTimeTo.IsZero() && f.ModTimeTo.Before(f.ModTimeFrom) {
			log.Fatal("filter: --min-age can't be larger than --max-age")
		}
		fs.Debugf(nil, "--max-age %v to %v", f.Opt.MaxAge, f.ModTimeFrom)
	}

	addImplicitExclude := false
	foundExcludeRule := false

	for _, rule := range f.Opt.IncludeRule {
		err = f.Add(true, rule)
		if err != nil {
			return nil, err
		}
		addImplicitExclude = true
	}
	for _, rule := range f.Opt.IncludeFrom {
		err := forEachLine(rule, false, func(line string) error {
			return f.Add(true, line)
		})
		if err != nil {
			return nil, err
		}
		addImplicitExclude = true
	}
	for _, rule := range f.Opt.ExcludeRule {
		err = f.Add(false, rule)
		if err != nil {
			return nil, err
		}
		foundExcludeRule = true
	}
	for _, rule := range f.Opt.ExcludeFrom {
		err := forEachLine(rule, false, func(line string) error {
			return f.Add(false, line)
		})
		if err != nil {
			return nil, err
		}
		foundExcludeRule = true
	}

	if addImplicitExclude && foundExcludeRule {
		fs.Errorf(nil, "Using --filter is recommended instead of both --include and --exclude as the order they are parsed in is indeterminate")
	}

	for _, rule := range f.Opt.FilterRule {
		err = f.AddRule(rule)
		if err != nil {
			return nil, err
		}
	}
	for _, rule := range f.Opt.FilterFrom {
		err := forEachLine(rule, false, f.AddRule)
		if err != nil {
			return nil, err
		}
	}

	inActive := f.InActive()

	for _, rule := range f.Opt.FilesFrom {
		if !inActive {
			return nil, fmt.Errorf("The usage of --files-from overrides all other filters, it should be used alone or with --files-from-raw")
		}
		f.initAddFile() // init to show --files-from set even if no files within
		err := forEachLine(rule, false, func(line string) error {
			return f.AddFile(line)
		})
		if err != nil {
			return nil, err
		}
	}

	for _, rule := range f.Opt.FilesFromRaw {
		// --files-from-raw can be used with --files-from, hence we do
		// not need to get the value of f.InActive again
		if !inActive {
			return nil, fmt.Errorf("The usage of --files-from-raw overrides all other filters, it should be used alone or with --files-from")
		}
		f.initAddFile() // init to show --files-from set even if no files within
		err := forEachLine(rule, true, func(line string) error {
			return f.AddFile(line)
		})
		if err != nil {
			return nil, err
		}
	}

	if addImplicitExclude {
		err = f.Add(false, "/**")
		if err != nil {
			return nil, err
		}
	}
	if fs.GetConfig(context.Background()).Dump&fs.DumpFilters != 0 {
		fmt.Println("--- start filters ---")
		fmt.Println(f.DumpFilters())
		fmt.Println("--- end filters ---")
	}
	return f, nil
}

func mustNewFilter(opt *Opt) *Filter {
	f, err := NewFilter(opt)
	if err != nil {
		panic(err)
	}
	return f
}

// addDirGlobs adds directory globs from the file glob passed in
func (f *Filter) addDirGlobs(Include bool, glob string) error {
	for _, dirGlob := range globToDirGlobs(glob) {
		// Don't add "/" as we always include the root
		if dirGlob == "/" {
			continue
		}
		dirRe, err := GlobToRegexp(dirGlob, f.Opt.IgnoreCase)
		if err != nil {
			return err
		}
		f.dirRules.add(Include, dirRe)
	}
	return nil
}

// Add adds a filter rule with include or exclude status indicated
func (f *Filter) Add(Include bool, glob string) error {
	isDirRule := strings.HasSuffix(glob, "/")
	isFileRule := !isDirRule
	// Make excluding "dir/" equivalent to excluding "dir/**"
	if isDirRule && !Include {
		glob += "**"
	}
	if strings.Contains(glob, "**") {
		isDirRule, isFileRule = true, true
	}
	re, err := GlobToRegexp(glob, f.Opt.IgnoreCase)
	if err != nil {
		return err
	}
	if isFileRule {
		f.fileRules.add(Include, re)
		// If include rule work out what directories are needed to scan
		// if exclude rule, we can't rule anything out
		// Unless it is `*` which matches everything
		// NB ** and /** are DirRules
		if Include || glob == "*" {
			err = f.addDirGlobs(Include, glob)
			if err != nil {
				return err
			}
		}
	}
	if isDirRule {
		f.dirRules.add(Include, re)
	}
	return nil
}

// AddRule adds a filter rule with include/exclude indicated by the prefix
//
// These are
//
//   + glob
//   - glob
//   !
//
// '+' includes the glob, '-' excludes it and '!' resets the filter list
//
// Line comments may be introduced with '#' or ';'
func (f *Filter) AddRule(rule string) error {
	switch {
	case rule == "!":
		f.Clear()
		return nil
	case strings.HasPrefix(rule, "- "):
		return f.Add(false, rule[2:])
	case strings.HasPrefix(rule, "+ "):
		return f.Add(true, rule[2:])
	}
	return fmt.Errorf("malformed rule %q", rule)
}

// initAddFile creates f.files and f.dirs
func (f *Filter) initAddFile() {
	if f.files == nil {
		f.files = make(FilesMap)
		f.dirs = make(FilesMap)
	}
}

// AddFile adds a single file to the files from list
func (f *Filter) AddFile(file string) error {
	f.initAddFile()
	file = strings.Trim(file, "/")
	f.files[file] = struct{}{}
	// Put all the parent directories into f.dirs
	for {
		file = path.Dir(file)
		if file == "." {
			break
		}
		if _, found := f.dirs[file]; found {
			break
		}
		f.dirs[file] = struct{}{}
	}
	return nil
}

// Files returns all the files from the `--files-from` list
//
// It may be nil if the list is empty
func (f *Filter) Files() FilesMap {
	return f.files
}

// Clear clears all the filter rules
func (f *Filter) Clear() {
	f.fileRules.clear()
	f.dirRules.clear()
}

// InActive returns false if any filters are active
func (f *Filter) InActive() bool {
	return (f.files == nil &&
		f.ModTimeFrom.IsZero() &&
		f.ModTimeTo.IsZero() &&
		f.Opt.MinSize < 0 &&
		f.Opt.MaxSize < 0 &&
		f.fileRules.len() == 0 &&
		f.dirRules.len() == 0 &&
		len(f.Opt.ExcludeFile) == 0)
}

// IncludeRemote returns whether this remote passes the filter rules.
func (f *Filter) IncludeRemote(remote string) bool {
	for _, rule := range f.fileRules.rules {
		if rule.Match(remote) {
			return rule.Include
		}
	}
	return true
}

// ListContainsExcludeFile checks if exclude file is present in the list.
func (f *Filter) ListContainsExcludeFile(entries fs.DirEntries) bool {
	if len(f.Opt.ExcludeFile) == 0 {
		return false
	}
	for _, entry := range entries {
		obj, ok := entry.(fs.Object)
		if ok {
			basename := path.Base(obj.Remote())
			if basename == f.Opt.ExcludeFile {
				return true
			}
		}
	}
	return false
}

// IncludeDirectory returns a function which checks whether this
// directory should be included in the sync or not.
func (f *Filter) IncludeDirectory(ctx context.Context, fs fs.Fs) func(string) (bool, error) {
	return func(remote string) (bool, error) {
		remote = strings.Trim(remote, "/")
		// first check if we need to remove directory based on
		// the exclude file
		excl, err := f.DirContainsExcludeFile(ctx, fs, remote)
		if err != nil {
			return false, err
		}
		if excl {
			return false, nil
		}

		// filesFrom takes precedence
		if f.files != nil {
			_, include := f.dirs[remote]
			return include, nil
		}
		remote += "/"
		for _, rule := range f.dirRules.rules {
			if rule.Match(remote) {
				return rule.Include, nil
			}
		}

		return true, nil
	}
}

// DirContainsExcludeFile checks if exclude file is present in a
// directory. If fs is nil, it works properly if ExcludeFile is an
// empty string (for testing).
func (f *Filter) DirContainsExcludeFile(ctx context.Context, fremote fs.Fs, remote string) (bool, error) {
	if len(f.Opt.ExcludeFile) > 0 {
		exists, err := fs.FileExists(ctx, fremote, path.Join(remote, f.Opt.ExcludeFile))
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

// Include returns whether this object should be included into the
// sync or not
func (f *Filter) Include(remote string, size int64, modTime time.Time) bool {
	// filesFrom takes precedence
	if f.files != nil {
		_, include := f.files[remote]
		return include
	}
	if !f.ModTimeFrom.IsZero() && modTime.Before(f.ModTimeFrom) {
		return false
	}
	if !f.ModTimeTo.IsZero() && modTime.After(f.ModTimeTo) {
		return false
	}
	if f.Opt.MinSize >= 0 && size < int64(f.Opt.MinSize) {
		return false
	}
	if f.Opt.MaxSize >= 0 && size > int64(f.Opt.MaxSize) {
		return false
	}
	return f.IncludeRemote(remote)
}

// IncludeObject returns whether this object should be included into
// the sync or not. This is a convenience function to avoid calling
// o.ModTime(), which is an expensive operation.
func (f *Filter) IncludeObject(ctx context.Context, o fs.Object) bool {
	var modTime time.Time

	if !f.ModTimeFrom.IsZero() || !f.ModTimeTo.IsZero() {
		modTime = o.ModTime(ctx)
	} else {
		modTime = time.Unix(0, 0)
	}

	return f.Include(o.Remote(), o.Size(), modTime)
}

// forEachLine calls fn on every line in the file pointed to by path
//
// It ignores empty lines and lines starting with '#' or ';' if raw is false
func forEachLine(path string, raw bool, fn func(string) error) (err error) {
	var scanner *bufio.Scanner
	if path == "-" {
		scanner = bufio.NewScanner(os.Stdin)
	} else {
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner = bufio.NewScanner(in)
		defer fs.CheckClose(in, &err)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !raw {
			line = strings.TrimSpace(line)
			if len(line) == 0 || line[0] == '#' || line[0] == ';' {
				continue
			}
		}
		err := fn(line)
		if err != nil {
			return err
		}
	}
	return scanner.Err()
}

// DumpFilters dumps the filters in textual form, 1 per line
func (f *Filter) DumpFilters() string {
	rules := []string{}
	if !f.ModTimeFrom.IsZero() {
		rules = append(rules, fmt.Sprintf("Last-modified date must be equal or greater than: %s", f.ModTimeFrom.String()))
	}
	if !f.ModTimeTo.IsZero() {
		rules = append(rules, fmt.Sprintf("Last-modified date must be equal or less than: %s", f.ModTimeTo.String()))
	}
	rules = append(rules, "--- File filter rules ---")
	for _, rule := range f.fileRules.rules {
		rules = append(rules, rule.String())
	}
	rules = append(rules, "--- Directory filter rules ---")
	for _, dirRule := range f.dirRules.rules {
		rules = append(rules, dirRule.String())
	}
	return strings.Join(rules, "\n")
}

// HaveFilesFrom returns true if --files-from has been supplied
func (f *Filter) HaveFilesFrom() bool {
	return f.files != nil
}

var errFilesFromNotSet = errors.New("--files-from not set so can't use Filter.ListR")

// MakeListR makes function to return all the files set using --files-from
func (f *Filter) MakeListR(ctx context.Context, NewObject func(ctx context.Context, remote string) (fs.Object, error)) fs.ListRFn {
	return func(ctx context.Context, dir string, callback fs.ListRCallback) error {
		ci := fs.GetConfig(ctx)
		if !f.HaveFilesFrom() {
			return errFilesFromNotSet
		}
		var (
			checkers = ci.Checkers
			remotes  = make(chan string, checkers)
			g        errgroup.Group
		)
		for i := 0; i < checkers; i++ {
			g.Go(func() (err error) {
				var entries = make(fs.DirEntries, 1)
				for remote := range remotes {
					entries[0], err = NewObject(ctx, remote)
					if err == fs.ErrorObjectNotFound {
						// Skip files that are not found
					} else if err != nil {
						return err
					} else {
						err = callback(entries)
						if err != nil {
							return err
						}
					}
				}
				return nil
			})
		}
		for remote := range f.files {
			remotes <- remote
		}
		close(remotes)
		return g.Wait()
	}
}

// UsesDirectoryFilters returns true if the filter uses directory
// filters and false if it doesn't.
//
// This is used in deciding whether to walk directories or use ListR
func (f *Filter) UsesDirectoryFilters() bool {
	if len(f.dirRules.rules) == 0 {
		return false
	}
	rule := f.dirRules.rules[0]
	re := rule.Regexp.String()
	if rule.Include == true && re == "^.*$" {
		return false
	}
	return true
}

// Context key for config
type configContextKeyType struct{}

var configContextKey = configContextKeyType{}

// GetConfig returns the global or context sensitive config
func GetConfig(ctx context.Context) *Filter {
	if ctx == nil {
		return globalConfig
	}
	c := ctx.Value(configContextKey)
	if c == nil {
		return globalConfig
	}
	return c.(*Filter)
}

// CopyConfig copies the global config (if any) from srcCtx into
// dstCtx returning the new context.
func CopyConfig(dstCtx, srcCtx context.Context) context.Context {
	if srcCtx == nil {
		return dstCtx
	}
	c := srcCtx.Value(configContextKey)
	if c == nil {
		return dstCtx
	}
	return context.WithValue(dstCtx, configContextKey, c)
}

// AddConfig returns a mutable config structure based on a shallow
// copy of that found in ctx and returns a new context with that added
// to it.
func AddConfig(ctx context.Context) (context.Context, *Filter) {
	c := GetConfig(ctx)
	cCopy := new(Filter)
	*cCopy = *c
	newCtx := context.WithValue(ctx, configContextKey, cCopy)
	return newCtx, cCopy
}

// ReplaceConfig replaces the filter config in the ctx with the one
// passed in and returns a new context with that added to it.
func ReplaceConfig(ctx context.Context, f *Filter) context.Context {
	newCtx := context.WithValue(ctx, configContextKey, f)
	return newCtx
}

// Context key for the "use filter" flag
type useFlagContextKeyType struct{}

var useFlagContextKey = useFlagContextKeyType{}

// GetUseFilter obtains the "use filter" flag from context
// The flag tells filter-aware backends (Drive) to constrain List using filter
func GetUseFilter(ctx context.Context) bool {
	if ctx != nil {
		if pVal := ctx.Value(useFlagContextKey); pVal != nil {
			return *(pVal.(*bool))
		}
	}
	return false
}

// SetUseFilter returns a context having (re)set the "use filter" flag
func SetUseFilter(ctx context.Context, useFilter bool) context.Context {
	if useFilter == GetUseFilter(ctx) {
		return ctx // Minimize depth of nested contexts
	}
	pVal := new(bool)
	*pVal = useFilter
	return context.WithValue(ctx, useFlagContextKey, pVal)
}
