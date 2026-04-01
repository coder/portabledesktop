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
		thumbnailFile      string
		thumbnailWidth     int
		thumbnailHeight    int
	)

	cmd := &cobra.Command{
		Use:   "record [file]",
		Short: "Record the desktop to a video file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (thumbnailWidth > 0 || thumbnailHeight > 0) && thumbnailFile == "" {
				return fmt.Errorf("--thumbnail is required when --thumbnail-width or --thumbnail-height is set")
			}
			if thumbnailWidth > 0 && thumbnailHeight > 0 {
				return fmt.Errorf("--thumbnail-width and --thumbnail-height are mutually exclusive")
			}

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

			if thumbnailFile != "" {
				thumbOpts := desktop.ThumbnailOptions{InputFile: handle.File}
				if thumbnailWidth > 0 {
					thumbOpts.Width = &thumbnailWidth
				}
				if thumbnailHeight > 0 {
					thumbOpts.Height = &thumbnailHeight
				}
				jpegData, err := d.Thumbnail(thumbOpts)
				if err != nil {
					return fmt.Errorf("extract thumbnail: %w", err)
				}
				if err := os.WriteFile(thumbnailFile, jpegData, 0o644); err != nil {
					return fmt.Errorf("write thumbnail: %w", err)
				}
				fmt.Fprintf(stdout, "thumbnail: %s\n", thumbnailFile)
			}

			fmt.Fprintf(stdout, "saved: %s\n", handle.File)
			return nil
		},
	}

	cmd.Flags().IntVar(&fps, "fps", 30, "recording frames per second")
	cmd.Flags().Float64Var(&idleSpeedup, "idle-speedup", 0, "Idle segment playback acceleration factor (e.g. 20). Disabled when <= 1.")
	cmd.Flags().Float64Var(&idleMinDuration, "idle-min-duration", 0, "Minimum idle segment duration in seconds before acceleration")
	cmd.Flags().StringVar(&idleNoiseTolerance, "idle-noise-tolerance", "", "ffmpeg freezedetect noise tolerance (e.g. -38dB)")
	cmd.Flags().StringVar(&thumbnailFile, "thumbnail", "", "write a JPEG thumbnail of the first frame to this path")
	cmd.Flags().IntVar(&thumbnailWidth, "thumbnail-width", 0, "resize thumbnail to this width (preserves aspect ratio)")
	cmd.Flags().IntVar(&thumbnailHeight, "thumbnail-height", 0, "resize thumbnail to this height (preserves aspect ratio)")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
