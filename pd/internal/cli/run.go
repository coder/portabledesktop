package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

func newRunCommand(stdout, stderr io.Writer) *cobra.Command {
	var (
		cwd           string
		timeoutMs     int
		jsonOutput    bool
		allowNonZero  bool
		stateFile     string
	)

	cmd := &cobra.Command{
		Use:   "run <command> [args...]",
		Short: "Run a command inside the desktop and capture output",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			ctx := context.Background()
			if timeoutMs > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(
					ctx,
					time.Duration(timeoutMs)*time.Millisecond,
				)
				defer cancel()
			}

			proc := exec.CommandContext(ctx, args[0], args[1:]...)
			proc.Env = d.Env()
			if cwd != "" {
				proc.Dir = cwd
			}

			var stdoutBuf, stderrBuf bytes.Buffer
			proc.Stdout = &stdoutBuf
			proc.Stderr = &stderrBuf

			runErr := proc.Run()

			exitCode := 0
			if runErr != nil {
				if ee, ok := runErr.(*exec.ExitError); ok {
					exitCode = ee.ExitCode()
				} else {
					return runErr
				}
			}

			if !allowNonZero && exitCode != 0 {
				errOutput := stderrBuf.String()
				if errOutput == "" {
					errOutput = stdoutBuf.String()
				}
				return fmt.Errorf(
					"command exited with code %d: %s",
					exitCode, errOutput,
				)
			}

			if jsonOutput {
				result := struct {
					ExitCode int    `json:"code"`
					Stdout   string `json:"stdout"`
					Stderr   string `json:"stderr"`
					Signal   string `json:"signal,omitempty"`
				}{
					ExitCode: exitCode,
					Stdout:   stdoutBuf.String(),
					Stderr:   stderrBuf.String(),
				}
				enc := json.NewEncoder(stdout)
				if err := enc.Encode(result); err != nil {
					return err
				}
			} else {
				_, _ = io.Copy(stdout, &stdoutBuf)
				if stderrBuf.Len() > 0 {
					_, _ = io.Copy(stderr, &stderrBuf)
				}
				if allowNonZero && exitCode != 0 {
					fmt.Fprintf(stderr, "command exited with code %d\n", exitCode)
				}
			}

			if allowNonZero && exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory")
	cmd.Flags().IntVar(&timeoutMs, "timeout-ms", 0, "timeout in milliseconds (0 = no timeout)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&allowNonZero, "allow-non-zero", false, "do not error on non-zero exit codes")
	addStateFileFlag(cmd, &stateFile)
	return cmd
}
