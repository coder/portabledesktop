package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/spf13/cobra"
)

func newDownCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop a running desktop session",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			if err := d.Kill(desktop.KillOptions{}); err != nil {
				return err
			}

			_ = os.Remove(stateFile)
			fmt.Fprintln(stdout, "stopped")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}
