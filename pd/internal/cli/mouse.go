package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
)

func newMouseCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mouse",
		Short: "Mouse input commands",
	}

	cmd.AddCommand(
		newMouseMoveCommand(stdout, stderr),
		newMouseClickCommand(stdout, stderr),
		newMouseDownCommand(stdout, stderr),
		newMouseUpCommand(stdout, stderr),
		newMouseScrollCommand(stdout, stderr),
	)
	return cmd
}

func newMouseMoveCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "move <x> <y>",
		Short: "Move the mouse to absolute coordinates",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			x, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid x coordinate: %w", err)
			}
			y, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid y coordinate: %w", err)
			}

			if err := d.MoveMouse(x, y); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newMouseClickCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "click [button]",
		Short: "Click a mouse button (default: left)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			button := "left"
			if len(args) > 0 {
				button = args[0]
			}

			if err := d.Click(button); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newMouseDownCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "down [button]",
		Short: "Press a mouse button without releasing",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			button := "left"
			if len(args) > 0 {
				button = args[0]
			}

			if err := d.MouseDown(button); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newMouseUpCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "up [button]",
		Short: "Release a mouse button",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			button := "left"
			if len(args) > 0 {
				button = args[0]
			}

			if err := d.MouseUp(button); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}

func newMouseScrollCommand(stdout, stderr io.Writer) *cobra.Command {
	var stateFile string

	cmd := &cobra.Command{
		Use:   "scroll [dx] [dy]",
		Short: "Scroll the mouse wheel",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := loadDesktopFromState(stateFile)
			if err != nil {
				return err
			}

			dx, dy := 0, 0
			if len(args) > 0 {
				v, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid dx: %w", err)
				}
				dx = v
			}
			if len(args) > 1 {
				v, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("invalid dy: %w", err)
				}
				dy = v
			}

			if err := d.Scroll(dx, dy); err != nil {
				return err
			}
			fmt.Fprint(stdout, "ok\n")
			return nil
		},
	}

	addStateFileFlag(cmd, &stateFile)
	return cmd
}
