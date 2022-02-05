package vfsflags

import (
	"fmt"
	"os"
	"strconv"
)

// FileMode is a command line friendly os.FileMode
type FileMode struct {
	Mode *os.FileMode
}

// String turns FileMode into a string
func (x *FileMode) String() string {
	return fmt.Sprintf("0%3o", *x.Mode)
}

// Set a FileMode
func (x *FileMode) Set(s string) error {
	i, err := strconv.ParseInt(s, 8, 64)
	if err != nil {
		return fmt.Errorf("Bad FileMode - must be octal digits: %w", err)
	}
	*x.Mode = (os.FileMode)(i)
	return nil
}

// Type of the value
func (x *FileMode) Type() string {
	return "FileMode"
}
