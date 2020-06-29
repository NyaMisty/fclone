// +build cmount
// +build cgo
// +build linux darwin freebsd windows
// +build !race !windows

// FIXME this doesn't work with the race detector under Windows either
// hanging or producing lots of differences.

package cmount

import (
	"testing"

<<<<<<< .merge_file_a08800
=======
	"github.com/rclone/rclone/fstest/testy"
>>>>>>> .merge_file_a33992
	"github.com/rclone/rclone/vfs/vfstest"
)

func TestMount(t *testing.T) {
<<<<<<< .merge_file_a08800
=======
	testy.SkipUnreliable(t)
>>>>>>> .merge_file_a33992
	vfstest.RunTests(t, false, mount)
}
