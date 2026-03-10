package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newCursorCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		jsonOutput bool
		stateFile  string
	)

	cmd := &cobra.Command{
		Use:   "cursor",
		Short: "Get the current mouse cursor position",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			x, y, err := d.MousePosition()
			if err != nil {
				return err
			}

			if jsonOutput {
				result := struct {
					X int `json:"x"`
					Y int `json:"y"`
				}{X: x, Y: y}
				enc := json.NewEncoder(stdout)
				return enc.Encode(result)
			}

			fmt.Fprintf(stdout, "%d,%d\n", x, y)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
