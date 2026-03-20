package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/freetype-go/freetype/truetype"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/motif"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgbutil/xwindow"
)

const (
	windowTitle            = "PortableDesktop Click Probe"
	windowClass            = "clickprobe"
	eventPollInterval      = 50 * time.Millisecond
	readyPollTimeout       = 10 * time.Second
	defaultMarkerInset     = 24
	defaultMarkerSize      = 4
	statusMessageBottomPad = 48
	statusMessageLifetime  = 10 * time.Second
	statusMessageFadeTime  = 1 * time.Second
	statusMessageFontSize  = 24
	statusMessageBoxHPad   = 20
	statusMessageBoxVPad   = 12
	statusMessageShadowPad = 2
)

var (
	backgroundColor   = color.RGBA{R: 0x11, G: 0x2A, B: 0x46, A: 0xFF}
	markerColor       = color.RGBA{R: 0xFF, G: 0x7A, B: 0x00, A: 0xFF}
	statusTextColor   = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	statusShadowColor = color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF}
	statusBoxColor    = color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xB8}
)

type markerPosition struct {
	name string
	x    int
	y    int
}

func markerPositions(bounds image.Rectangle) []markerPosition {
	return []markerPosition{
		{
			name: "top-left",
			x:    bounds.Min.X + defaultMarkerInset,
			y:    bounds.Min.Y + defaultMarkerInset,
		},
		{
			name: "top-right",
			x:    bounds.Min.X + bounds.Dx() - defaultMarkerInset - markerSize,
			y:    bounds.Min.Y + defaultMarkerInset,
		},
		{
			name: "bottom-left",
			x:    bounds.Min.X + defaultMarkerInset,
			y:    bounds.Min.Y + bounds.Dy() - defaultMarkerInset - markerSize,
		},
		{
			name: "bottom-right",
			x:    bounds.Min.X + bounds.Dx() - defaultMarkerInset - markerSize,
			y:    bounds.Min.Y + bounds.Dy() - defaultMarkerInset - markerSize,
		},
		{
			name: "center",
			x:    bounds.Min.X + (bounds.Dx()-markerSize)/2,
			y:    bounds.Min.Y + (bounds.Dy()-markerSize)/2,
		},
	}
}

func markerRect(pos markerPosition) image.Rectangle {
	return image.Rect(pos.x, pos.y, pos.x+markerSize, pos.y+markerSize)
}

// statusMessage tracks the currently visible click confirmation banner.
type statusMessage struct {
	text    string
	shownAt time.Time
}

type readyEvent struct {
	Type                string `json:"type"`
	ScreenWidth         int    `json:"screenWidth"`
	ScreenHeight        int    `json:"screenHeight"`
	WindowX             int    `json:"windowX"`
	WindowY             int    `json:"windowY"`
	WindowWidth         int    `json:"windowWidth"`
	WindowHeight        int    `json:"windowHeight"`
	FullscreenRequested bool   `json:"fullscreenRequested"`
}

type configureEvent struct {
	Type         string `json:"type"`
	WindowX      int    `json:"windowX"`
	WindowY      int    `json:"windowY"`
	WindowWidth  int    `json:"windowWidth"`
	WindowHeight int    `json:"windowHeight"`
}

type clickEvent struct {
	Type         string `json:"type"`
	Seq          int    `json:"seq"`
	Button       int    `json:"button"`
	RootX        int    `json:"rootX"`
	RootY        int    `json:"rootY"`
	EventX       int    `json:"eventX"`
	EventY       int    `json:"eventY"`
	WindowWidth  int    `json:"windowWidth"`
	WindowHeight int    `json:"windowHeight"`
}

type eventLogger struct {
	encoder *json.Encoder
	file    *os.File
}

func newEventLogger(path string) (*eventLogger, error) {
	if path == "" {
		return nil, fmt.Errorf("--events-file is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create events directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events file: %w", err)
	}
	return &eventLogger{encoder: json.NewEncoder(file), file: file}, nil
}

func (l *eventLogger) log(v any) error {
	if err := l.encoder.Encode(v); err != nil {
		return err
	}
	return l.file.Sync()
}

func (l *eventLogger) close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

type app struct {
	X                   *xgbutil.XUtil
	window              *xwindow.Window
	rootGeom            image.Rectangle
	rootWindow          xproto.Window
	canvas              *xgraphics.Image
	events              *eventLogger
	readyFile           string
	fullscreenRequested bool
	readyEmitted        bool
	clickSeq            int
	font                *truetype.Font
	status              *statusMessage
	renderedStatusText  string
	renderedStatusAlpha uint8
}

var (
	markerSize  int
	borderWidth int
)

func main() {
	var (
		eventsFile string
		readyFile  string
	)

	flag.StringVar(&eventsFile, "events-file", "", "path to JSONL event log")
	flag.StringVar(&readyFile, "ready-file", "", "path to touch after ready event")
	flag.IntVar(&markerSize, "marker-size", defaultMarkerSize, "marker side length in pixels")
	flag.IntVar(&borderWidth, "border-width", 1, "screen border width in pixels")
	flag.Parse()

	logger, err := newEventLogger(eventsFile)
	if err != nil {
		log.Fatal(err)
	}
	defer logger.close()

	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatalf("connect to X server: %v", err)
	}
	defer X.Conn().Close()

	app, err := newApp(X, logger, readyFile)
	if err != nil {
		log.Fatalf("initialize click probe: %v", err)
	}
	defer app.destroy()

	if err := app.run(); err != nil {
		log.Fatalf("run click probe: %v", err)
	}
}

func newApp(X *xgbutil.XUtil, events *eventLogger, readyFile string) (*app, error) {
	rootGeom, err := xwindow.RawGeometry(X, xproto.Drawable(X.RootWin()))
	if err != nil {
		return nil, fmt.Errorf("query root geometry: %w", err)
	}

	window, err := xwindow.Generate(X)
	if err != nil {
		return nil, fmt.Errorf("generate window: %w", err)
	}

	font, err := loadStatusFont()
	if err != nil {
		return nil, err
	}

	canvas := xgraphics.New(X, image.Rect(0, 0, rootGeom.Width(), rootGeom.Height()))
	fillBackground(canvas)
	drawMarkers(canvas)
	drawBorder(canvas)

	valueMask := xproto.CwBackPixel | xproto.CwBorderPixel
	valueList := []uint32{0, 0}
	if err := window.CreateChecked(
		X.RootWin(),
		0,
		0,
		rootGeom.Width(),
		rootGeom.Height(),
		valueMask,
		valueList...,
	); err != nil {
		return nil, fmt.Errorf("create window: %w", err)
	}

	if err := window.Listen(
		xproto.EventMaskStructureNotify,
		xproto.EventMaskButtonPress,
		xproto.EventMaskExposure,
	); err != nil {
		return nil, fmt.Errorf("listen for events: %w", err)
	}

	application := &app{
		X:          X,
		window:     window,
		rootGeom:   image.Rect(0, 0, rootGeom.Width(), rootGeom.Height()),
		rootWindow: X.RootWin(),
		canvas:     canvas,
		events:     events,
		readyFile:  readyFile,
		font:       font,
	}

	if err := application.setWindowProperties(); err != nil {
		return nil, err
	}
	if err := application.paint(); err != nil {
		return nil, err
	}

	return application, nil
}

func (a *app) run() error {
	a.window.Map()
	a.X.Conn().Sync()

	if err := a.requestFullscreen(); err != nil {
		return err
	}

	a.window.Stack(xproto.StackModeAbove)
	a.window.Focus()
	a.X.Conn().Sync()

	deadline := time.Now().Add(readyPollTimeout)
	ticker := time.NewTicker(eventPollInterval)
	defer ticker.Stop()

	for {
		if err := a.pumpEvents(false); err != nil {
			return err
		}
		if err := a.maybeEmitReady(); err != nil {
			return err
		}
		if !a.readyEmitted && time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for fullscreen geometry")
		}
		if err := a.refreshStatusMessage(); err != nil {
			return err
		}
		<-ticker.C
	}
}

func (a *app) destroy() {
	if a.canvas != nil {
		a.canvas.Destroy()
	}
	if a.window != nil {
		a.window.Destroy()
	}
}

func (a *app) setWindowProperties() error {
	if err := icccm.WmStateSet(a.X, a.window.Id, &icccm.WmState{State: icccm.StateNormal}); err != nil {
		return fmt.Errorf("set WM_STATE: %w", err)
	}
	if err := icccm.WmClassSet(a.X, a.window.Id, &icccm.WmClass{Instance: windowClass, Class: windowClass}); err != nil {
		return fmt.Errorf("set WM_CLASS: %w", err)
	}
	if err := icccm.WmNameSet(a.X, a.window.Id, windowTitle); err != nil {
		return fmt.Errorf("set WM_NAME: %w", err)
	}
	if err := ewmh.WmNameSet(a.X, a.window.Id, windowTitle); err != nil {
		return fmt.Errorf("set _NET_WM_NAME: %w", err)
	}
	if err := ewmh.WmWindowTypeSet(a.X, a.window.Id, []string{"_NET_WM_WINDOW_TYPE_NORMAL"}); err != nil {
		return fmt.Errorf("set _NET_WM_WINDOW_TYPE: %w", err)
	}
	if err := motif.WmHintsSet(a.X, a.window.Id, &motif.Hints{Flags: motif.HintDecorations, Decoration: motif.DecorationNone}); err != nil {
		return fmt.Errorf("set _MOTIF_WM_HINTS: %w", err)
	}
	if err := ewmh.WmStateSet(a.X, a.window.Id, []string{"_NET_WM_STATE_FULLSCREEN"}); err != nil {
		return fmt.Errorf("set initial _NET_WM_STATE: %w", err)
	}
	return nil
}

func (a *app) paint() error {
	fillBackground(a.canvas)
	drawMarkers(a.canvas)
	drawBorder(a.canvas)

	statusText, statusAlpha, statusVisible := statusMessageState(a.status, time.Now())
	if statusVisible {
		if err := a.drawStatusMessage(statusText, statusAlpha); err != nil {
			return err
		}
	}

	if err := a.canvas.XSurfaceSet(a.window.Id); err != nil {
		return fmt.Errorf("set paint surface: %w", err)
	}
	a.canvas.XDraw()
	a.canvas.XPaint(a.window.Id)
	a.X.Conn().Sync()

	if statusVisible {
		a.renderedStatusText = statusText
		a.renderedStatusAlpha = statusAlpha
	} else {
		a.renderedStatusText = ""
		a.renderedStatusAlpha = 0
	}

	return nil
}

func (a *app) requestFullscreen() error {
	a.fullscreenRequested = true
	if err := ewmh.WmStateReq(a.X, a.window.Id, ewmh.StateAdd, "_NET_WM_STATE_FULLSCREEN"); err != nil {
		return fmt.Errorf("request fullscreen: %w", err)
	}
	if err := ewmh.ActiveWindowReq(a.X, a.window.Id); err != nil {
		return fmt.Errorf("request active window: %w", err)
	}
	a.window.Stack(xproto.StackModeAbove)
	a.window.Focus()
	a.X.Conn().Sync()
	return nil
}

func (a *app) maybeEmitReady() error {
	if a.readyEmitted {
		return nil
	}
	geom, err := a.window.Geometry()
	if err != nil {
		return fmt.Errorf("query window geometry: %w", err)
	}
	rootX, rootY, err := a.windowRootOrigin()
	if err != nil {
		return err
	}
	if rootX != 0 || rootY != 0 {
		return nil
	}
	if geom.Width() != a.rootGeom.Dx() || geom.Height() != a.rootGeom.Dy() {
		return nil
	}

	if err := a.paint(); err != nil {
		return fmt.Errorf("paint fullscreen window: %w", err)
	}

	ready := readyEvent{
		Type:                "ready",
		ScreenWidth:         a.rootGeom.Dx(),
		ScreenHeight:        a.rootGeom.Dy(),
		WindowX:             rootX,
		WindowY:             rootY,
		WindowWidth:         geom.Width(),
		WindowHeight:        geom.Height(),
		FullscreenRequested: a.fullscreenRequested,
	}
	if err := a.events.log(ready); err != nil {
		return fmt.Errorf("log ready event: %w", err)
	}
	if a.readyFile != "" {
		if err := os.MkdirAll(filepath.Dir(a.readyFile), 0o755); err != nil {
			return fmt.Errorf("create ready directory: %w", err)
		}
		if err := os.WriteFile(a.readyFile, []byte("ready\n"), 0o644); err != nil {
			return fmt.Errorf("write ready file: %w", err)
		}
	}
	a.readyEmitted = true
	return nil
}

func (a *app) windowRootOrigin() (int, int, error) {
	reply, err := xproto.TranslateCoordinates(a.X.Conn(), a.window.Id, a.rootWindow, 0, 0).Reply()
	if err != nil {
		return 0, 0, fmt.Errorf("translate window coordinates: %w", err)
	}
	return int(reply.DstX), int(reply.DstY), nil
}

func (a *app) pumpEvents(block bool) error {
	if block {
		ev, err := a.X.Conn().WaitForEvent()
		if err != nil {
			return fmt.Errorf("wait for X event: %w", err)
		}
		if ev != nil {
			if err := a.handleEvent(ev); err != nil {
				return err
			}
		}
	}

	for {
		ev, err := a.X.Conn().PollForEvent()
		if err != nil {
			return fmt.Errorf("poll X event: %w", err)
		}
		if ev == nil {
			return nil
		}
		if err := a.handleEvent(ev); err != nil {
			return err
		}
	}
}

func loadStatusFont() (*truetype.Font, error) {
	for _, path := range statusFontCandidates() {
		font, err := parseFontFile(path)
		if err == nil {
			return font, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load status font %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("load status font: no usable font found")
}

func statusFontCandidates() []string {
	shareDirs := []string{}
	if runtimeDir := strings.TrimSpace(os.Getenv("PORTABLEDESKTOP_RUNTIME_DIR")); runtimeDir != "" {
		shareDirs = append(shareDirs, filepath.Join(runtimeDir, "share"))
	}
	for _, dir := range strings.Split(os.Getenv("XDG_DATA_DIRS"), string(os.PathListSeparator)) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		shareDirs = append(shareDirs, dir)
	}
	shareDirs = append(shareDirs, "/usr/local/share", "/usr/share")
	shareDirs = uniqueNonEmptyStrings(shareDirs)

	paths := make([]string, 0, len(shareDirs)+2)
	for _, dir := range shareDirs {
		paths = append(paths, filepath.Join(dir, "fonts", "truetype", "DejaVuSans.ttf"))
	}
	paths = append(paths,
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
	)
	return uniqueNonEmptyStrings(paths)
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func parseFontFile(path string) (*truetype.Font, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	font, err := xgraphics.ParseFont(file)
	if err != nil {
		return nil, err
	}
	return font, nil
}

func (a *app) refreshStatusMessage() error {
	text, alpha, visible := statusMessageState(a.status, time.Now())
	if !visible {
		a.status = nil
		if a.renderedStatusText == "" && a.renderedStatusAlpha == 0 {
			return nil
		}
		return a.paint()
	}
	if text == a.renderedStatusText && alpha == a.renderedStatusAlpha {
		return nil
	}
	return a.paint()
}

func statusMessageState(message *statusMessage, now time.Time) (string, uint8, bool) {
	if message == nil {
		return "", 0, false
	}

	elapsed := now.Sub(message.shownAt)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= statusMessageLifetime {
		return "", 0, false
	}

	alpha := uint8(0xFF)
	remaining := statusMessageLifetime - elapsed
	if remaining < statusMessageFadeTime {
		alpha = uint8((remaining * 255) / statusMessageFadeTime)
		if alpha == 0 {
			return "", 0, false
		}
	}

	return message.text, alpha, true
}

func (a *app) drawStatusMessage(text string, alpha uint8) error {
	if a.font == nil || text == "" || alpha == 0 {
		return nil
	}

	textWidth, textHeight := xgraphics.Extents(a.font, statusMessageFontSize, text)
	if textWidth <= 0 || textHeight <= 0 {
		return nil
	}

	boxWidth := textWidth + statusMessageBoxHPad*2
	boxHeight := textHeight + statusMessageBoxVPad*2
	left := (a.canvas.Bounds().Dx() - boxWidth) / 2
	top := a.canvas.Bounds().Dy() - statusMessageBottomPad - boxHeight
	box := image.Rect(left, top, left+boxWidth, top+boxHeight).Intersect(a.canvas.Bounds())

	drawFilledRect(a.canvas, box, compositeOver(backgroundColor, withScaledAlpha(statusBoxColor, alpha)))

	textX := left + statusMessageBoxHPad
	textY := top + statusMessageBoxVPad
	if _, _, err := a.canvas.Text(
		textX+statusMessageShadowPad,
		textY+statusMessageShadowPad,
		withScaledAlpha(statusShadowColor, alpha),
		statusMessageFontSize,
		a.font,
		text,
	); err != nil {
		return fmt.Errorf("draw status shadow: %w", err)
	}
	if _, _, err := a.canvas.Text(
		textX,
		textY,
		withScaledAlpha(statusTextColor, alpha),
		statusMessageFontSize,
		a.font,
		text,
	); err != nil {
		return fmt.Errorf("draw status text: %w", err)
	}
	return nil
}

func withScaledAlpha(clr color.RGBA, alpha uint8) color.RGBA {
	return color.RGBA{
		R: clr.R,
		G: clr.G,
		B: clr.B,
		A: uint8((uint16(clr.A) * uint16(alpha)) / 255),
	}
}

func compositeOver(background color.RGBA, overlay color.RGBA) color.RGBA {
	alpha := uint16(overlay.A)
	invAlpha := 255 - alpha
	return color.RGBA{
		R: uint8((uint16(overlay.R)*alpha + uint16(background.R)*invAlpha + 127) / 255),
		G: uint8((uint16(overlay.G)*alpha + uint16(background.G)*invAlpha + 127) / 255),
		B: uint8((uint16(overlay.B)*alpha + uint16(background.B)*invAlpha + 127) / 255),
		A: 0xFF,
	}
}

func drawFilledRect(dst *xgraphics.Image, rect image.Rectangle, clr color.RGBA) {
	fill := xgraphics.BGRA{B: clr.B, G: clr.G, R: clr.R, A: clr.A}
	rect = rect.Intersect(dst.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			dst.SetBGRA(x, y, fill)
		}
	}
}

func clickStatusMessage(root image.Rectangle, x, y int) string {
	clickPoint := image.Pt(x, y)
	for _, pos := range markerPositions(root) {
		if clickPoint.In(markerRect(pos)) {
			return pos.name + " click registered"
		}
	}
	return ""
}

func (a *app) showStatusMessage(text string) error {
	if text == "" {
		return nil
	}
	a.status = &statusMessage{text: text, shownAt: time.Now()}
	return a.paint()
}

func (a *app) handleEvent(event any) error {
	switch ev := event.(type) {
	case xproto.ExposeEvent:
		if ev.Window == a.window.Id {
			if err := a.paint(); err != nil {
				return err
			}
		}
	case xproto.ConfigureNotifyEvent:
		if ev.Window == a.window.Id {
			if err := a.events.log(configureEvent{
				Type:         "configure",
				WindowX:      int(ev.X),
				WindowY:      int(ev.Y),
				WindowWidth:  int(ev.Width),
				WindowHeight: int(ev.Height),
			}); err != nil {
				return fmt.Errorf("log configure event: %w", err)
			}
			if err := a.maybeEmitReady(); err != nil {
				return err
			}
		}
	case xproto.ButtonPressEvent:
		if ev.Event == a.window.Id {
			return a.logClick(ev)
		}
	case xproto.MapNotifyEvent:
		if ev.Window == a.window.Id {
			a.window.Stack(xproto.StackModeAbove)
			a.window.Focus()
			a.X.Conn().Sync()
		}
	}
	return nil
}

func (a *app) logClick(ev xproto.ButtonPressEvent) error {
	a.clickSeq++
	geom, err := a.window.Geometry()
	if err != nil {
		return fmt.Errorf("query geometry for click log: %w", err)
	}
	entry := clickEvent{
		Type:         "click",
		Seq:          a.clickSeq,
		Button:       int(ev.Detail),
		RootX:        int(ev.RootX),
		RootY:        int(ev.RootY),
		EventX:       int(ev.EventX),
		EventY:       int(ev.EventY),
		WindowWidth:  geom.Width(),
		WindowHeight: geom.Height(),
	}
	if err := a.events.log(entry); err != nil {
		return fmt.Errorf("log click event: %w", err)
	}
	if err := a.showStatusMessage(clickStatusMessage(a.rootGeom, entry.RootX, entry.RootY)); err != nil {
		return err
	}
	return nil
}

func fillBackground(dst *xgraphics.Image) {
	bg := xgraphics.BGRA{B: backgroundColor.B, G: backgroundColor.G, R: backgroundColor.R, A: backgroundColor.A}
	dst.For(func(x, y int) xgraphics.BGRA {
		return bg
	})
}

func drawBorder(dst *xgraphics.Image) {
	bounds := dst.Bounds()
	red := xgraphics.BGRA{B: 0x00, G: 0x00, R: 0xFF, A: 0xFF}
	// Top and bottom edges.
	for i := 0; i < borderWidth; i++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.SetBGRA(x, bounds.Min.Y+i, red)
			dst.SetBGRA(x, bounds.Max.Y-1-i, red)
		}
	}
	// Left and right edges.
	for i := 0; i < borderWidth; i++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			dst.SetBGRA(bounds.Min.X+i, y, red)
			dst.SetBGRA(bounds.Max.X-1-i, y, red)
		}
	}
}

func drawMarkers(dst *xgraphics.Image) {
	for _, pos := range markerPositions(dst.Bounds()) {
		drawMarker(dst, pos.x, pos.y)
	}
}

func drawMarker(dst *xgraphics.Image, x, y int) {
	marker := xgraphics.BGRA{B: markerColor.B, G: markerColor.G, R: markerColor.R, A: markerColor.A}
	for my := 0; my < markerSize; my++ {
		for mx := 0; mx < markerSize; mx++ {
			dst.SetBGRA(x+mx, y+my, marker)
		}
	}
}
