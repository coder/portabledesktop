package cli

import (
	"io"

	"github.com/coder/portabledesktop/pd/internal/desktop"
	"github.com/coder/portabledesktop/pd/internal/session"
	"github.com/spf13/cobra"
)

// EmbeddedRuntime is set by main.go before calling Run(). It
// contains the compressed runtime blob that EnsureRuntime unpacks.
var EmbeddedRuntime []byte

// Run executes the CLI with the given args and output writers.
// This is the main entry point called by main.go and by tests.
func Run(args []string, stdout, stderr io.Writer) error {
	cmd := newRootCommand(stdout, stderr)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd.Execute()
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "portabledesktop",
		Short:         "Portable Linux desktop runtime CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Register all subcommands.
	root.AddCommand(
		newUpCommand(stdout, stderr),
		newDownCommand(stdout, stderr),
		newInfoCommand(stdout, stderr),
		newOpenCommand(stdout, stderr),
		newRunCommand(stdout, stderr),
		newScreenshotCommand(stdout, stderr),
		newRecordCommand(stdout, stderr),
		newViewerCommand(stdout, stderr),
		newMouseCommand(stdout, stderr),
		newKeyboardCommand(stdout, stderr),
		newCursorCommand(stdout, stderr),
		newBackgroundCommand(stdout, stderr),
		newBackgroundImageCommand(stdout, stderr),
		newCacheCommand(stdout, stderr),
	)
	return root
}

// addStateFileFlag registers the --state-file flag on cmd.
func addStateFileFlag(cmd *cobra.Command, stateFile *string) {
	cmd.Flags().StringVar(
		stateFile, "state-file",
		session.DefaultStateFilePath(),
		"path to desktop state file",
	)
}

// loadDesktopFromState reads the session state file and
// reconstructs a Desktop value that can drive the running
// session.
func loadDesktopFromState(
	stateFile string,
) (*desktop.Desktop, *session.StoredDesktopState, error) {
	state, err := session.LoadState(stateFile)
	if err != nil {
		return nil, nil, err
	}
	d := &desktop.Desktop{
		RuntimeDir:        state.RuntimeDir,
		Display:           state.Display,
		VNCPort:           state.VNCPort,
		Geometry:          state.Geometry,
		Depth:             state.Depth,
		DPI:               state.DPI,
		DesktopSizeMode:   state.DesktopSizeMode,
		SessionDir:        state.SessionDir,
		CleanupSessionDir: state.CleanupSessionDir,
		Detached:          state.Detached,
	}
	if state.XvncPid != nil {
		d.XvncPid = *state.XvncPid
	}
	if state.OpenboxPid != nil {
		d.OpenboxPid = *state.OpenboxPid
	}
	return d, &state, nil
}
