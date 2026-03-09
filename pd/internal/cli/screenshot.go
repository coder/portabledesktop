package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/spf13/cobra"
)

func newScreenshotCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		file                      string
		jsonOutput                bool
		x, y                      int
		width, height             int
		scaleToGeometry           bool
		timeoutMs                 int
		targetWidth, targetHeight int
		stateFile                 string
	)

	cmd := &cobra.Command{
		Use:   "screenshot [file]",
		Short: "Capture a PNG screenshot of the desktop",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			// Positional file argument overrides the flag.
			outFile := file
			if len(args) > 0 {
				outFile = args[0]
			}

			opts := desktop.ScreenshotOptions{
				ScaleToGeometry: scaleToGeometry,
				TimeoutMs:       timeoutMs,
			}

			// If any region flag is set, all four must be
			// provided.
			regionSet := cmd.Flags().Changed("x") ||
				cmd.Flags().Changed("y") ||
				cmd.Flags().Changed("width") ||
				cmd.Flags().Changed("height")

			if regionSet {
				if !cmd.Flags().Changed("x") ||
					!cmd.Flags().Changed("y") ||
					!cmd.Flags().Changed("width") ||
					!cmd.Flags().Changed("height") {
					return fmt.Errorf(
						"all four region flags (--x, --y, --width, --height) must be set together",
					)
				}
				region := [4]int{x, y, x + width, y + height}
				opts.Region = &region
			}

			if cmd.Flags().Changed("target-width") || cmd.Flags().Changed("target-height") {
				if !cmd.Flags().Changed("target-width") || !cmd.Flags().Changed("target-height") {
					return fmt.Errorf("--target-width and --target-height must be set together")
				}
				opts.TargetWidth = &targetWidth
				opts.TargetHeight = &targetHeight
			}

			pngData, err := d.Screenshot(opts)
			if err != nil {
				return err
			}

			// Write to file.
			if outFile != "" {
				if err := os.WriteFile(outFile, pngData, 0o644); err != nil {
					return fmt.Errorf("write screenshot: %w", err)
				}
				if jsonOutput {
					result := struct {
						File string `json:"file"`
						Size int    `json:"size"`
					}{
						File: outFile,
						Size: len(pngData),
					}
					enc := json.NewEncoder(stdout)
					return enc.Encode(result)
				}
				fmt.Fprintf(stdout, "%s\n", outFile)
				return nil
			}

			// No file: output base64.
			b64 := base64.StdEncoding.EncodeToString(pngData)
			if jsonOutput {
				result := struct {
					Data string `json:"data"`
					Size int    `json:"size"`
				}{
					Data: b64,
					Size: len(pngData),
				}
				enc := json.NewEncoder(stdout)
				return enc.Encode(result)
			}

			fmt.Fprintln(stdout, b64)
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "output file path")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().IntVar(&x, "x", 0, "region crop x offset")
	cmd.Flags().IntVar(&y, "y", 0, "region crop y offset")
	cmd.Flags().IntVar(&width, "width", 0, "region crop width")
	cmd.Flags().IntVar(&height, "height", 0, "region crop height")
	cmd.Flags().BoolVar(&scaleToGeometry, "scale-to-geometry", false, "scale cropped region back to full geometry")
	cmd.Flags().IntVar(&timeoutMs, "timeout-ms", 0, "ffmpeg timeout in milliseconds")
	cmd.Flags().IntVar(&targetWidth, "target-width", 0, "final resize target width")
	cmd.Flags().IntVar(&targetHeight, "target-height", 0, "final resize target height")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
