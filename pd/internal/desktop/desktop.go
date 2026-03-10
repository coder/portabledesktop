package desktop

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/portabledesktop/pd/internal/runtime"
)

// Desktop holds the state for a running portable desktop session.
type Desktop struct {
	RuntimeDir        string
	Display           int
	VNCPort           int
	Geometry          string
	Depth             int
	DPI               int
	DesktopSizeMode   string // "fixed" or "dynamic"
	SessionDir        string
	CleanupSessionDir bool
	Detached          bool
	XvncPid           int
	OpenboxPid        int
	recordingCmd      *exec.Cmd
}

// StartOptions mirrors the TypeScript StartOptions and controls how
// a new desktop session is created.
type StartOptions struct {
	RuntimeDir      string
	SessionDir      string
	Cleanup         *bool
	Timeout         time.Duration
	Display         *int
	Port            *int
	Geometry        string
	Depth           int
	DPI             int
	DesktopSizeMode string
	XvncArgs        []string
	Openbox         *bool
	Detached        bool
	Background      *BackgroundOptions
}

// KillOptions controls the behaviour of Desktop.Kill.
type KillOptions struct {
	Cleanup *bool
}

// Start creates and starts a new portable desktop session.
func Start(runtimeDir string, opts StartOptions) (*Desktop, error) {
	// 1. Pick display + port.
	display, port, err := PickDisplayAndPort(opts.Display, opts.Port)
	if err != nil {
		return nil, fmt.Errorf("pick display/port: %w", err)
	}

	// 2. Session directory.
	sessionDir := opts.SessionDir
	autoSession := sessionDir == ""
	if autoSession {
		tmp, err := os.MkdirTemp("", "portabledesktop-")
		if err != nil {
			return nil, fmt.Errorf("create temp session dir: %w", err)
		}
		sessionDir = tmp
	} else {
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			return nil, fmt.Errorf("create session dir: %w", err)
		}
	}

	cleanupSession := autoSession
	if opts.Cleanup != nil {
		cleanupSession = *opts.Cleanup
	}

	// Defaults.
	geometry := opts.Geometry
	if geometry == "" {
		geometry = "1280x800"
	}
	depth := opts.Depth
	if depth == 0 {
		depth = 24
	}
	dpi := opts.DPI
	if dpi == 0 {
		dpi = 96
	}
	desktopSizeMode := opts.DesktopSizeMode
	if desktopSizeMode == "" {
		desktopSizeMode = "fixed"
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	acceptResize := "0"
	if desktopSizeMode == "dynamic" {
		acceptResize = "1"
	}

	// 3. Build Xvnc argument list.
	xvncArgs := []string{
		fmt.Sprintf(":%d", display),
		"-geometry", geometry,
		"-depth", strconv.Itoa(depth),
		"-dpi", strconv.Itoa(dpi),
		"-rfbport", strconv.Itoa(port),
		"-SecurityTypes", "None",
		"-ac",
		"-nolisten", "tcp",
		"-localhost", "no",
		fmt.Sprintf("-AcceptSetDesktopSize=%s", acceptResize),
	}
	xvncArgs = append(xvncArgs, opts.XvncArgs...)

	// 4. Spawn Xvnc.
	xvncBin := runtime.ResolveRuntimeBinary(runtimeDir, "Xvnc")
	xvncLog, err := os.OpenFile(
		filepath.Join(sessionDir, "xvnc.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644,
	)
	if err != nil {
		return nil, fmt.Errorf("open xvnc log: %w", err)
	}
	defer xvncLog.Close()

	xvncCmd := exec.Command(xvncBin, xvncArgs...)
	xvncCmd.Stdout = xvncLog
	xvncCmd.Stderr = xvncLog

	// Prepend runtimeDir/bin to PATH for the Xvnc process itself.
	runtimeBin := filepath.Join(runtimeDir, "bin")
	pathEnv := runtimeBin
	if cur := os.Getenv("PATH"); cur != "" {
		pathEnv = runtimeBin + ":" + cur
	}
	xvncCmd.Env = append(os.Environ(), "PATH="+pathEnv)

	if opts.Detached {
		xvncCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}

	if err := xvncCmd.Start(); err != nil {
		return nil, fmt.Errorf("start Xvnc: %w", err)
	}

	xvncPid := xvncCmd.Process.Pid

	// 5. Wait for VNC port to become reachable.
	if err := waitForPort("127.0.0.1", port, timeout); err != nil {
		// Best-effort cleanup.
		_ = killPid(xvncPid, 5*time.Second)
		return nil, err
	}

	// 6. Optionally start openbox (defaults to ON, matching TS
	// behavior where openbox is started unless explicitly disabled).
	var openboxPid int
	if opts.Openbox == nil || *opts.Openbox {
		openboxBin := runtime.ResolveRuntimeBinary(runtimeDir, "openbox")
		openboxLog, err := os.OpenFile(
			filepath.Join(sessionDir, "openbox.log"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644,
		)
		if err != nil {
			return nil, fmt.Errorf("open openbox log: %w", err)
		}
		defer openboxLog.Close()

		openboxCmd := exec.Command(openboxBin)
		openboxCmd.Stdout = openboxLog
		openboxCmd.Stderr = openboxLog
		openboxCmd.Env = BuildEnv(runtimeDir, display, nil)
		if opts.Detached {
			openboxCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		}

		if err := openboxCmd.Start(); err != nil {
			return nil, fmt.Errorf("start openbox: %w", err)
		}
		openboxPid = openboxCmd.Process.Pid
	}

	d := &Desktop{
		RuntimeDir:        runtimeDir,
		Display:           display,
		VNCPort:           port,
		Geometry:          geometry,
		Depth:             depth,
		DPI:               dpi,
		DesktopSizeMode:   desktopSizeMode,
		SessionDir:        sessionDir,
		CleanupSessionDir: cleanupSession,
		Detached:          opts.Detached,
		XvncPid:           xvncPid,
		OpenboxPid:        openboxPid,
	}

	// 7. Optional background.
	if opts.Background != nil {
		if err := d.SetBackground(*opts.Background); err != nil {
			return nil, fmt.Errorf("set background: %w", err)
		}
	}

	return d, nil
}

// Kill stops the desktop session. It stops any active recording,
// then terminates openbox and Xvnc, and optionally removes the
// session directory.
func (d *Desktop) Kill(opts KillOptions) error {
	// Stop recording if active, with a timeout to avoid blocking
	// forever (matches TS stopRecordingInternal timeout).
	if d.recordingCmd != nil && d.recordingCmd.Process != nil {
		_ = d.recordingCmd.Process.Signal(syscall.SIGINT)
		done := make(chan error, 1)
		go func() {
			done <- d.recordingCmd.Wait()
		}()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			_ = d.recordingCmd.Process.Kill()
			<-done
		}
		d.recordingCmd = nil
	}

	var errs []error

	if d.OpenboxPid != 0 {
		if err := killPid(d.OpenboxPid, 5*time.Second); err != nil {
			errs = append(errs, fmt.Errorf("kill openbox: %w", err))
		}
		d.OpenboxPid = 0
	}
	if d.XvncPid != 0 {
		if err := killPid(d.XvncPid, 5*time.Second); err != nil {
			errs = append(errs, fmt.Errorf("kill xvnc: %w", err))
		}
		d.XvncPid = 0
	}

	cleanup := d.CleanupSessionDir
	if opts.Cleanup != nil {
		cleanup = *opts.Cleanup
	}
	if cleanup {
		_ = os.RemoveAll(d.SessionDir)
	}

	return errors.Join(errs...)
}

// Env returns an environment slice suitable for child processes that
// need access to this desktop's display.
func (d *Desktop) Env() []string {
	return BuildEnv(d.RuntimeDir, d.Display, nil)
}

// runTool resolves a binary from the runtime directory and executes
// it, returning an error if the command fails.
func (d *Desktop) runTool(name string, args []string) error {
	_, err := d.runToolCapture(name, args)
	return err
}

// runToolCapture resolves a binary from the runtime directory,
// executes it, and returns its combined stdout. Returns an error if
// the command exits with a non-zero status.
func (d *Desktop) runToolCapture(name string, args []string) (string, error) {
	bin := runtime.ResolveRuntimeBinary(d.RuntimeDir, name)
	cmd := exec.Command(bin, args...)
	cmd.Env = d.Env()

	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return "", fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(stderr))
	}
	return string(out), nil
}

// captureSize returns the current display dimensions by querying
// xdotool, falling back to parsing d.Geometry.
func (d *Desktop) captureSize() (width, height int, err error) {
	out, runErr := d.runToolCapture("xdotool", []string{"getdisplaygeometry"})
	if runErr == nil {
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) == 2 {
			w, e1 := strconv.Atoi(parts[0])
			h, e2 := strconv.Atoi(parts[1])
			if e1 == nil && e2 == nil && w > 0 && h > 0 {
				return w, h, nil
			}
		}
	}

	// Fallback to parsing the configured geometry.
	return parseGeometry(d.Geometry)
}

// waitForPort polls a TCP endpoint with exponential backoff until a
// connection succeeds or the timeout expires.
func waitForPort(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	attempt := 0

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		backoff := time.Duration(math.Min(
			float64(500*time.Millisecond),
			float64(50*time.Millisecond)*math.Pow(2, float64(attempt)),
		))
		jitter := time.Duration(rand.Intn(30)) * time.Millisecond //nolint:gosec
		backoff += jitter
		if backoff > remaining {
			backoff = remaining
		}
		attempt++
		time.Sleep(backoff)
	}

	return fmt.Errorf("timed out waiting for TCP %s after %s", addr, timeout)
}

// killPid sends SIGTERM, polls for exit, then falls back to SIGKILL.
// It returns nil when the process is successfully stopped or was
// already gone, and an error only for unexpected signal failures.
func killPid(pid int, timeout time.Duration) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// SIGTERM.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("sigterm pid %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Signal 0 checks whether the process exists.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return nil // exited
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Fallback to SIGKILL.
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("sigkill pid %d: %w", pid, err)
	}
	return nil
}

// parseGeometry parses a "WxH" geometry string.
func parseGeometry(s string) (width, height int, err error) {
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid geometry: %s, expected WxH", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid geometry width: %w", err)
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid geometry height: %w", err)
	}
	if w < 1 || h < 1 {
		return 0, 0, fmt.Errorf("invalid geometry dimensions: %s", s)
	}
	return w, h, nil
}

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(b bool) *bool { return &b }
