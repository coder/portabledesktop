package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/spf13/cobra"
)

func newRecordCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		fps                int
		idleSpeedup        float64
		idleMinDuration    float64
		idleNoiseTolerance string
		stateFile          string
	)

	cmd := &cobra.Command{
		Use:   "record [file]",
		Short: "Record the desktop to a video file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			opts := desktop.RecordingOptions{
				FPS:                fps,
				IdleSpeedup:        idleSpeedup,
				IdleMinDurationSec: idleMinDuration,
				IdleNoiseTolerance: idleNoiseTolerance,
			}
			if len(args) > 0 {
				opts.File = args[0]
			}

			handle, err := d.StartRecording(opts)
			if err != nil {
				return err
			}

			fmt.Fprintf(stdout, "recording: %s\n", handle.File)
			fmt.Fprintln(stdout, "press Ctrl+C to stop")

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			if err := handle.Stop(); err != nil {
				return err
			}

			fmt.Fprintf(stdout, "saved: %s\n", handle.File)
			return nil
		},
	}

	cmd.Flags().IntVar(&fps, "fps", 30, "recording frames per second")
	cmd.Flags().Float64Var(&idleSpeedup, "idle-speedup", 0, "Idle segment playback acceleration factor (e.g. 20). Disabled when <= 1.")
	cmd.Flags().Float64Var(&idleMinDuration, "idle-min-duration", 0, "Minimum idle segment duration in seconds before acceleration")
	cmd.Flags().StringVar(&idleNoiseTolerance, "idle-noise-tolerance", "", "ffmpeg freezedetect noise tolerance (e.g. -38dB)")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
