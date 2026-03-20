package desktop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ActiveStateFile is set by the caller after starting a desktop session
// so that subsequent CLI commands target the correct instance.
var ActiveStateFile string

func portabledesktopBin() string {
	if v := os.Getenv("PORTABLEDESKTOP_BIN"); v != "" {
		return v
	}
	return "portabledesktop"
}

func LoadEnvLocal() {
	candidates := []string{
		filepath.Join("..", "..", ".env.local"),
		filepath.Join("..", "..", "..", ".env.local"),
	}
	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			idx := strings.Index(line, "=")
			if idx == -1 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
		break
	}
}

type Info struct {
	RuntimeDir              string `json:"runtimeDir"`
	Display                 int    `json:"display"`
	VNCPort                 int    `json:"vncPort"`
	Geometry                string `json:"geometry"`
	Depth                   int    `json:"depth"`
	DPI                     int    `json:"dpi"`
	DesktopSizeMode         string `json:"desktopSizeMode"`
	SessionDir              string `json:"sessionDir"`
	CleanupSessionDirOnStop bool   `json:"cleanupSessionDirOnStop"`
	Detached                bool   `json:"detached"`
	StateFile               string `json:"stateFile"`
	StartedAt               string `json:"startedAt"`
}

type Session struct {
	Info *Info
	cmd  *exec.Cmd
}

func StartDesktop(geometry, background string) (*Session, error) {
	bin := portabledesktopBin()
	args := []string{"up", "--json", "--foreground"}
	if geometry != "" {
		args = append(args, "--geometry", geometry)
	}
	if background != "" {
		args = append(args, "--background", background)
	}

	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start portabledesktop: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("no output from portabledesktop up")
	}

	var info Info
	if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("parse desktop info: %w", err)
	}

	return &Session{Info: &info, cmd: cmd}, nil
}

func (s *Session) Stop() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	_ = s.cmd.Wait()
	s.cmd = nil
}

func withActiveStateFile(args []string) []string {
	if ActiveStateFile == "" || len(args) == 0 {
		return args
	}
	withState := make([]string, 0, len(args)+2)
	withState = append(withState, args[0], "--state-file", ActiveStateFile)
	withState = append(withState, args[1:]...)
	return withState
}

func Exec(args ...string) (string, error) {
	cmd := exec.Command(portabledesktopBin(), withActiveStateFile(args)...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s: %s", err, string(ee.Stderr))
		}
		return "", err
	}
	return string(out), nil
}

func ExecVoid(args ...string) error {
	_, err := Exec(args...)
	return err
}

type RecordingHandle struct {
	cmd *exec.Cmd
}

func StartRecording(file string) *RecordingHandle {
	args := withActiveStateFile([]string{
		"record",
		"--idle-speedup", "20",
		"--idle-min-duration", "0.35",
		"--idle-noise-tolerance", "-38dB",
		file,
	})
	cmd := exec.Command(portabledesktopBin(), args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
	return &RecordingHandle{cmd: cmd}
}

func (r *RecordingHandle) Stop() {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return
	}
	_ = r.cmd.Process.Signal(syscall.SIGINT)
	_ = r.cmd.Wait()
	r.cmd = nil
}

func StartViewer(port int) *exec.Cmd {
	args := withActiveStateFile([]string{
		"viewer",
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"--no-open",
	})
	cmd := exec.Command(portabledesktopBin(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	_ = cmd.Start()
	return cmd
}

func StopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func OpenHostBrowser(url string) {
	var commands [][]string
	if runtime.GOOS == "darwin" {
		commands = [][]string{{"open", url}}
	} else {
		commands = [][]string{{"xdg-open", url}, {"sensible-browser", url}}
	}
	for _, c := range commands {
		cmd := exec.Command(c[0], c[1:]...)
		if cmd.Start() == nil {
			_ = cmd.Process.Release()
			return
		}
	}
	fmt.Fprintf(os.Stdout, "  Open manually: %s\n", url)
}
