// +build linux darwin,amd64

package mount2

import (
	"testing"

<<<<<<< .merge_file_a29832
=======
	"github.com/rclone/rclone/fstest/testy"
>>>>>>> .merge_file_a34496
	"github.com/rclone/rclone/vfs/vfstest"
)

func TestMount(t *testing.T) {
<<<<<<< .merge_file_a29832
=======
	testy.SkipUnreliable(t)
>>>>>>> .merge_file_a34496
	vfstest.RunTests(t, false, mount)
}
