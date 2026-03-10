package cli

import (
	"fmt"
	"io"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/spf13/cobra"
)

func newBackgroundCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "background <color>",
		Short: "Set a solid background color",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			opts := desktop.BackgroundOptions{
				Color: args[0],
			}
			if err := d.SetBackground(opts); err != nil {
				return err
			}

			fmt.Fprint(stdout, "background updated\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}
