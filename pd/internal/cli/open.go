package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newOpenCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		cwd       string
		stateFile string
	)

	cmd := &cobra.Command{
		Use:   "open <command> [args...]",
		Short: "Spawn a detached process inside the desktop",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			command := args[0]
			commandArgs := args[1:]

			// For Chrome/Chromium, auto-add --user-data-dir if
			// the user didn't already provide one.
			base := filepath.Base(command)
			if strings.Contains(base, "chrome") ||
				strings.Contains(base, "chromium") {
				hasUserDataDir := false
				for _, a := range commandArgs {
					if strings.HasPrefix(a, "--user-data-dir") {
						hasUserDataDir = true
						break
					}
				}
				if !hasUserDataDir {
					dir := filepath.Join(
						d.SessionDir,
						"profiles",
						fmt.Sprintf("chrome-%d", time.Now().UnixMilli()),
					)
					commandArgs = append(
						commandArgs,
						"--user-data-dir="+dir,
					)
				}
			}

			proc := exec.Command(command, commandArgs...)
			proc.Env = d.Env()
			proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if cwd != "" {
				proc.Dir = cwd
			}

			if err := proc.Start(); err != nil {
				return fmt.Errorf("start process: %w", err)
			}

			result := struct {
				PID     int      `json:"pid"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}{
				PID:     proc.Process.Pid,
				Command: command,
				Args:    commandArgs,
			}

			enc := json.NewEncoder(stdout)
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory for the spawned process")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
