package cli

import (
	"fmt"
	"io"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/spf13/cobra"
)

func newBackgroundImageCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		mode      string
		stateFile string
	)

	cmd := &cobra.Command{
		Use:   "background-image <file>",
		Short: "Set a background image",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			opts := desktop.BackgroundOptions{
				ImagePath: args[0],
				Mode:      mode,
			}
			if err := d.SetBackground(opts); err != nil {
				return err
			}

			fmt.Fprint(stdout, "background updated\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "background image mode (center|fill|fit|stretch|tile)")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
