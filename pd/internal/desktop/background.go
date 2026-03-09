package desktop

// BackgroundOptions controls how the desktop background is set.
type BackgroundOptions struct {
	// Color is a solid colour specification (e.g. "#1e1e2e").
	Color string
	// ImagePath is the path to a background image file.
	ImagePath string
	// Mode controls how the image is rendered:
	// center, fill, fit, stretch, or tile.
	Mode string
}

// SetBackground sets the desktop background using xsetroot (for
// solid colours) or xwallpaper (for images).
func (d *Desktop) SetBackground(opts BackgroundOptions) error {
	if opts.ImagePath != "" {
		return d.setBackgroundImage(opts.ImagePath, opts.Mode)
	}
	if opts.Color != "" {
		return d.setBackgroundColor(opts.Color)
	}
	return nil
}

// setBackgroundColor uses xsetroot to paint a solid colour.
func (d *Desktop) setBackgroundColor(color string) error {
	return d.runTool("xsetroot", []string{"-solid", color})
}

// setBackgroundImage uses xwallpaper to set a background image.
func (d *Desktop) setBackgroundImage(path, mode string) error {
	flag := "--zoom"
	switch mode {
	case "center":
		flag = "--center"
	case "fill":
		flag = "--zoom"
	case "fit":
		flag = "--maximize"
	case "stretch":
		flag = "--stretch"
	case "tile":
		flag = "--tile"
	}

	return d.runTool("xwallpaper", []string{flag, path})
}
