package settier

import (
	"context"
	"fmt"

	"github.com/rclone/rclone/cmd"
	"github.com/rclone/rclone/fs/operations"
	"github.com/spf13/cobra"
)

func init() {
	cmd.Root.AddCommand(commandDefinition)
}

var commandDefinition = &cobra.Command{
	Use:   "settier tier remote:path",
	Short: `Changes storage class/tier of objects in remote.`,
	Long: `
rclone settier changes storage tier or class at remote if supported.
Few cloud storage services provides different storage classes on objects,
for example AWS S3 and Glacier, Azure Blob storage - Hot, Cool and Archive,
Google Cloud Storage, Regional Storage, Nearline, Coldline etc.

Note that, certain tier changes make objects not available to access immediately.
For example tiering to archive in azure blob storage makes objects in frozen state,
user can restore by setting tier to Hot/Cool, similarly S3 to Glacier makes object
inaccessible.true

You can use it to tier single object

    rclone settier Cool remote:path/file

Or use rclone filters to set tier on only specific files

	rclone --include "*.txt" settier Hot remote:path/dir

Or just provide remote directory and all files in directory will be tiered

    rclone settier tier remote:path/dir
`,
	Run: func(command *cobra.Command, args []string) {
		cmd.CheckArgs(2, 2, command, args)
		tier := args[0]
		input := args[1:]
		fsrc := cmd.NewFsSrc(input)
		cmd.Run(false, false, command, func() error {
			isSupported := fsrc.Features().SetTier
			if !isSupported {
				return fmt.Errorf("Remote %s does not support settier", fsrc.Name())
			}

			return operations.SetTier(context.Background(), fsrc, tier)
		})
	},
}
