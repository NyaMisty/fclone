// Build for cmount for unsupported platforms to stop go complaining
// about "no buildable Go source files "

//go:build (!linux && !darwin && !freebsd && !windows) || !brew || !cgo || !cmount
// +build !linux,!darwin,!freebsd,!windows !brew !cgo !cmount

package cmount
