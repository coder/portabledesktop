package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/coder/portabledesktop/pd/internal/runtime"
	"github.com/coder/portabledesktop/pd/internal/session"
	"github.com/spf13/cobra"
)

func newUpCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		jsonOutput      bool
		foreground      bool
		noOpenbox       bool
		xvncArgs        []string
		runtimeDir      string
		sessionDir      string
		displayFlag     int
		portFlag        int
		geometry        string
		depth           int
		dpi             int
		desktopSizeMode string
		background      string
		backgroundImage string
		backgroundMode  string
		stateFile       string
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start a desktop session",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate enum flags.
			switch desktopSizeMode {
			case "fixed", "dynamic":
				// valid
			default:
				return fmt.Errorf("invalid --desktop-size-mode %q: must be \"fixed\" or \"dynamic\"", desktopSizeMode)
			}

			if cmd.Flags().Changed("background-mode") {
				switch backgroundMode {
				case "center", "fill", "fit", "stretch", "tile":
					// valid
				default:
					return fmt.Errorf("invalid --background-mode %q: must be one of center, fill, fit, stretch, tile", backgroundMode)
				}
			}

			// Resolve the runtime directory. If the user
			// explicitly passed --runtime-dir, use it directly;
			// otherwise unpack the embedded blob.
			rtDir := runtimeDir
			if rtDir == "" {
				dir, err := runtime.EnsureRuntime(EmbeddedRuntime)
				if err != nil {
					return fmt.Errorf("ensure runtime: %w", err)
				}
				rtDir = dir
			} else {
				if err := runtime.ValidateRuntimeDir(rtDir); err != nil {
					return err
				}
			}

			// Build start options.
			opts := desktop.StartOptions{
				RuntimeDir:      rtDir,
				SessionDir:      sessionDir,
				Geometry:        geometry,
				Depth:           depth,
				DPI:             dpi,
				DesktopSizeMode: desktopSizeMode,
				XvncArgs:        xvncArgs,
				Openbox:         desktop.BoolPtr(!noOpenbox),
				Detached:        !foreground,
			}

			if cmd.Flags().Changed("display") {
				opts.Display = &displayFlag
			}
			if cmd.Flags().Changed("port") {
				opts.Port = &portFlag
			}

			if background != "" || backgroundImage != "" {
				bgOpts := desktop.BackgroundOptions{}
				if background != "" {
					bgOpts.Color = background
				}
				if backgroundImage != "" {
					bgOpts.ImagePath = backgroundImage
					bgOpts.Mode = backgroundMode
				}
				opts.Background = &bgOpts
			}

			d, err := desktop.Start(rtDir, opts)
			if err != nil {
				return err
			}

			// Save state.
			xvncPid := d.XvncPid
			openboxPid := d.OpenboxPid
			state := session.StoredDesktopState{
				RuntimeDir:        d.RuntimeDir,
				Display:           d.Display,
				VNCPort:           d.VNCPort,
				Geometry:          d.Geometry,
				Depth:             d.Depth,
				DPI:               d.DPI,
				DesktopSizeMode:   d.DesktopSizeMode,
				SessionDir:        d.SessionDir,
				CleanupSessionDir: d.CleanupSessionDir,
				XvncPid:           &xvncPid,
				Detached:          d.Detached,
				StateFile:         stateFile,
				StartedAt:         time.Now().UTC().Format(time.RFC3339),
			}
			if openboxPid != 0 {
				state.OpenboxPid = &openboxPid
			}
			if err := session.SaveState(stateFile, state); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			// Print info.
			if jsonOutput {
				enc := json.NewEncoder(stdout)
				if err := enc.Encode(state); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(stdout, "state: %s\n", stateFile)
				fmt.Fprintf(stdout, "display: :%d\n", d.Display)
				fmt.Fprintf(stdout, "vnc: 127.0.0.1:%d\n", d.VNCPort)
				fmt.Fprintf(stdout, "dpi: %d\n", d.DPI)
				fmt.Fprintf(stdout, "desktopSizeMode: %s\n", d.DesktopSizeMode)
				fmt.Fprintf(stdout, "runtime: %s\n", d.RuntimeDir)
				fmt.Fprintf(stdout, "session: %s\n", d.SessionDir)
			}

			// Foreground mode: block on signal then tear down.
			if foreground {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh

				_ = d.Kill(desktop.KillOptions{})
				_ = os.Remove(stateFile)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output session info as JSON")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in foreground; stop on signal")
	cmd.Flags().BoolVar(&noOpenbox, "no-openbox", false, "do not start the openbox window manager")
	cmd.Flags().StringSliceVar(&xvncArgs, "xvnc-arg", nil, "extra argument(s) to pass to Xvnc")
	cmd.Flags().StringVar(&runtimeDir, "runtime-dir", "", "path to runtime directory (skip embedded unpack)")
	cmd.Flags().StringVar(&sessionDir, "session-dir", "", "path to session directory")
	cmd.Flags().IntVar(&displayFlag, "display", 0, "X display number")
	cmd.Flags().IntVar(&portFlag, "port", 0, "VNC port number")
	cmd.Flags().StringVar(&geometry, "geometry", "1280x800", "desktop geometry WxH")
	cmd.Flags().IntVar(&depth, "depth", 24, "color depth")
	cmd.Flags().IntVar(&dpi, "dpi", 96, "dots per inch")
	cmd.Flags().StringVar(&desktopSizeMode, "desktop-size-mode", "fixed", "desktop size mode (fixed or dynamic)")
	cmd.Flags().StringVar(&background, "background", "", "solid background color")
	cmd.Flags().StringVar(&backgroundImage, "background-image", "", "background image file path")
	cmd.Flags().StringVar(&backgroundMode, "background-mode", "", "background image mode (center|fill|fit|stretch|tile)")
	addStateFileFlag(cmd, &stateFile)

	return cmd
}
