// Test Union filesystem interface
package union_test

import (
	"testing"

	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/memory"
	"github.com/rclone/rclone/backend/union"
	"github.com/rclone/rclone/fstest"
	"github.com/rclone/rclone/fstest/fstests"
)

// TestIntegration runs integration tests against the remote
func TestIntegration(t *testing.T) {
	if *fstest.RemoteName == "" {
		t.Skip("Skipping as -remote not set")
	}
	fstests.Run(t, &fstests.Opt{
		RemoteName:                   *fstest.RemoteName,
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}

func TestStandard(t *testing.T) {
	if *fstest.RemoteName != "" {
		t.Skip("Skipping as -remote set")
	}
	dirs, clean := union.MakeTestDirs(t, 3)
	defer clean()
	upstreams := dirs[0] + " " + dirs[1] + " " + dirs[2]
	name := "TestUnion"
	fstests.Run(t, &fstests.Opt{
		RemoteName: name + ":",
		ExtraConfig: []fstests.ExtraConfigItem{
			{Name: name, Key: "type", Value: "union"},
			{Name: name, Key: "upstreams", Value: upstreams},
			{Name: name, Key: "action_policy", Value: "epall"},
			{Name: name, Key: "create_policy", Value: "epmfs"},
			{Name: name, Key: "search_policy", Value: "ff"},
		},
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}

func TestRO(t *testing.T) {
	if *fstest.RemoteName != "" {
		t.Skip("Skipping as -remote set")
	}
	dirs, clean := union.MakeTestDirs(t, 3)
	defer clean()
	upstreams := dirs[0] + " " + dirs[1] + ":ro " + dirs[2] + ":ro"
	name := "TestUnionRO"
	fstests.Run(t, &fstests.Opt{
		RemoteName: name + ":",
		ExtraConfig: []fstests.ExtraConfigItem{
			{Name: name, Key: "type", Value: "union"},
			{Name: name, Key: "upstreams", Value: upstreams},
			{Name: name, Key: "action_policy", Value: "epall"},
			{Name: name, Key: "create_policy", Value: "epmfs"},
			{Name: name, Key: "search_policy", Value: "ff"},
		},
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}

func TestNC(t *testing.T) {
	if *fstest.RemoteName != "" {
		t.Skip("Skipping as -remote set")
	}
	dirs, clean := union.MakeTestDirs(t, 3)
	defer clean()
	upstreams := dirs[0] + " " + dirs[1] + ":nc " + dirs[2] + ":nc"
	name := "TestUnionNC"
	fstests.Run(t, &fstests.Opt{
		RemoteName: name + ":",
		ExtraConfig: []fstests.ExtraConfigItem{
			{Name: name, Key: "type", Value: "union"},
			{Name: name, Key: "upstreams", Value: upstreams},
			{Name: name, Key: "action_policy", Value: "epall"},
			{Name: name, Key: "create_policy", Value: "epmfs"},
			{Name: name, Key: "search_policy", Value: "ff"},
		},
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}

func TestPolicy1(t *testing.T) {
	if *fstest.RemoteName != "" {
		t.Skip("Skipping as -remote set")
	}
	dirs, clean := union.MakeTestDirs(t, 3)
	defer clean()
	upstreams := dirs[0] + " " + dirs[1] + " " + dirs[2]
	name := "TestUnionPolicy1"
	fstests.Run(t, &fstests.Opt{
		RemoteName: name + ":",
		ExtraConfig: []fstests.ExtraConfigItem{
			{Name: name, Key: "type", Value: "union"},
			{Name: name, Key: "upstreams", Value: upstreams},
			{Name: name, Key: "action_policy", Value: "all"},
			{Name: name, Key: "create_policy", Value: "lus"},
			{Name: name, Key: "search_policy", Value: "all"},
		},
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}

func TestPolicy2(t *testing.T) {
	if *fstest.RemoteName != "" {
		t.Skip("Skipping as -remote set")
	}
	dirs, clean := union.MakeTestDirs(t, 3)
	defer clean()
	upstreams := dirs[0] + " " + dirs[1] + " " + dirs[2]
	name := "TestUnionPolicy2"
	fstests.Run(t, &fstests.Opt{
		RemoteName: name + ":",
		ExtraConfig: []fstests.ExtraConfigItem{
			{Name: name, Key: "type", Value: "union"},
			{Name: name, Key: "upstreams", Value: upstreams},
			{Name: name, Key: "action_policy", Value: "all"},
			{Name: name, Key: "create_policy", Value: "rand"},
			{Name: name, Key: "search_policy", Value: "ff"},
		},
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}

func TestPolicy3(t *testing.T) {
	if *fstest.RemoteName != "" {
		t.Skip("Skipping as -remote set")
	}
	dirs, clean := union.MakeTestDirs(t, 3)
	defer clean()
	upstreams := dirs[0] + " " + dirs[1] + " " + dirs[2]
	name := "TestUnionPolicy3"
	fstests.Run(t, &fstests.Opt{
		RemoteName: name + ":",
		ExtraConfig: []fstests.ExtraConfigItem{
			{Name: name, Key: "type", Value: "union"},
			{Name: name, Key: "upstreams", Value: upstreams},
			{Name: name, Key: "action_policy", Value: "all"},
			{Name: name, Key: "create_policy", Value: "all"},
			{Name: name, Key: "search_policy", Value: "all"},
		},
		UnimplementableFsMethods:     []string{"OpenWriterAt", "DuplicateFiles"},
		UnimplementableObjectMethods: []string{"MimeType"},
	})
}
