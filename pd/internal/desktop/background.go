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
// solid colours) and/or xwallpaper (for images). When both Color
// and ImagePath are set, the colour is painted first as a solid
// fill and the image is then composited on top. This lets a small
// transparent or centered image (such as a logo) sit on a uniform
// coloured background without leaving uncovered root-window pixels.
func (d *Desktop) SetBackground(opts BackgroundOptions) error {
	if opts.Color != "" {
		if err := d.setBackgroundColor(opts.Color); err != nil {
			return err
		}
	}
	if opts.ImagePath != "" {
		return d.setBackgroundImage(opts.ImagePath, opts.Mode)
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
