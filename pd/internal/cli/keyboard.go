package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newKeyboardCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keyboard",
		Short: "Keyboard input commands",
	}

	cmd.AddCommand(
		newKeyboardTypeCommand(stdout, stderr),
		newKeyboardKeyCommand(stdout, stderr),
		newKeyboardDownCommand(stdout, stderr),
		newKeyboardUpCommand(stdout, stderr),
	)
	return cmd
}

func newKeyboardTypeCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "type <text...>",
		Short: "Type text with simulated keystrokes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			text := strings.Join(args, " ")
			if err := d.Type(text); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newKeyboardKeyCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "key <combo...>",
		Short: "Send a key combination",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			combo := strings.Join(args, " ")
			if err := d.Key(combo); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newKeyboardDownCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "down <key...>",
		Short: "Press a key without releasing",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			key := strings.Join(args, " ")
			if err := d.KeyDown(key); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newKeyboardUpCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "up <key...>",
		Short: "Release a previously pressed key",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			key := strings.Join(args, " ")
			if err := d.KeyUp(key); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}
