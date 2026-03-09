package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/coder/portabledesktop/pd/internal/viewer"
	"github.com/spf13/cobra"
)

func newViewerCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		host      string
		port      int
		scale     string
		noOpen    bool
		stateFile string
	)

	cmd := &cobra.Command{
		Use:   "viewer",
		Short: "Start an HTTP/WebSocket VNC viewer server",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate enum flags.
			switch scale {
			case "fit", "1:1":
				// valid
			default:
				return fmt.Errorf("invalid --scale %q: must be \"fit\" or \"1:1\"", scale)
			}

			_, state, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			cfg := viewer.Config{
				VNCHost:         "127.0.0.1",
				VNCPort:         state.VNCPort,
				ListenHost:      host,
				ListenPort:      port,
				Scale:           scale,
				DesktopSizeMode: state.DesktopSizeMode,
				AutoOpen:        !noOpen,
				Stdout:          stdout,
			}

			return viewer.Serve(ctx, cfg)
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "listen host")
	cmd.Flags().IntVar(&port, "port", 0, "listen port (0 = random)")
	cmd.Flags().StringVar(&scale, "scale", "fit", "viewer scale mode (fit or 1:1)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not auto-open a browser")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
