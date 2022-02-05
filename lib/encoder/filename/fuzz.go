//go:build gofuzz
// +build gofuzz

package filename

import (
	"bytes"
	"fmt"
)

// Run like:
// go-fuzz-build -o=fuzz-build.zip -func=Fuzz . && go-fuzz -minimize=5s -bin=fuzz-build.zip -workdir=testdata/corpus -procs=24

// Fuzz test the provided input.
func Fuzz(data []byte) int {
	// First try to decode as is.
	// We don't care about the result, it just shouldn't crash.
	Decode(string(data))

	// Now encode
	enc := Encode(string(data))

	// And decoded must match
	decoded, err := Decode(enc)
	if err != nil {
		panic(fmt.Sprintf("error decoding %q, input %q: %v", enc, string(data), err))
	}
	if !bytes.Equal(data, []byte(decoded)) {
		table := decodeMap[enc[0]]
		table--
		panic(fmt.Sprintf("decode mismatch, encoded: %q, org: %q, got: %q, table %d", enc, string(data), decoded, int(table)))
	}

	// Everything is good.
	return 1
}
