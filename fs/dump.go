package fs

import (
	"fmt"
	"strings"
)

// DumpFlags describes the Dump options in force
type DumpFlags int

// DumpFlags definitions
const (
	DumpHeaders DumpFlags = 1 << iota
	DumpBodies
	DumpRequests
	DumpResponses
	DumpAuth
	DumpFilters
	DumpGoRoutines
	DumpOpenFiles
)

var dumpFlags = []struct {
	flag DumpFlags
	name string
}{
	{DumpHeaders, "headers"},
	{DumpBodies, "bodies"},
	{DumpRequests, "requests"},
	{DumpResponses, "responses"},
	{DumpAuth, "auth"},
	{DumpFilters, "filters"},
	{DumpGoRoutines, "goroutines"},
	{DumpOpenFiles, "openfiles"},
}

// DumpFlagsList is a list of dump flags used in the help
var DumpFlagsList string

func init() {
	// calculate the dump flags list
	var out []string
	for _, info := range dumpFlags {
		out = append(out, info.name)
	}
	DumpFlagsList = strings.Join(out, ",")
}

// String turns a DumpFlags into a string
func (f DumpFlags) String() string {
	var out []string
	for _, info := range dumpFlags {
		if f&info.flag != 0 {
			out = append(out, info.name)
			f &^= info.flag
		}
	}
	if f != 0 {
		out = append(out, fmt.Sprintf("Unknown-0x%X", int(f)))
	}
	return strings.Join(out, ",")
}

// Set a DumpFlags as a comma separated list of flags
func (f *DumpFlags) Set(s string) error {
	var flags DumpFlags
	parts := strings.Split(s, ",")
	for _, part := range parts {
		found := false
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		for _, info := range dumpFlags {
			if part == info.name {
				found = true
				flags |= info.flag
			}
		}
		if !found {
			return fmt.Errorf("Unknown dump flag %q", part)
		}
	}
	*f = flags
	return nil
}

// Type of the value
func (f *DumpFlags) Type() string {
	return "DumpFlags"
}

// UnmarshalJSON makes sure the value can be parsed as a string or integer in JSON
func (f *DumpFlags) UnmarshalJSON(in []byte) error {
	return UnmarshalJSONFlag(in, f, func(i int64) error {
		*f = (DumpFlags)(i)
		return nil
	})
}
