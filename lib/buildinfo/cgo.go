//go:build cgo
// +build cgo

package buildinfo

func init() {
	Tags = append(Tags, "cgo")
}
