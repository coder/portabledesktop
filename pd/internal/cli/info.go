package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newInfoCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		jsonOutput bool
		stateFile  string
	)

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Print information about the running desktop session",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, state, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(stdout)
				return enc.Encode(state)
			}

			fmt.Fprintf(stdout, "state: %s\n", stateFile)
			fmt.Fprintf(stdout, "display: :%d\n", state.Display)
			fmt.Fprintf(stdout, "vnc: 127.0.0.1:%d\n", state.VNCPort)
			fmt.Fprintf(stdout, "dpi: %d\n", state.DPI)
			fmt.Fprintf(stdout, "desktopSizeMode: %s\n", state.DesktopSizeMode)
			fmt.Fprintf(stdout, "runtime: %s\n", state.RuntimeDir)
			fmt.Fprintf(stdout, "session: %s\n", state.SessionDir)
			fmt.Fprintf(stdout, "started: %s\n", state.StartedAt)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
