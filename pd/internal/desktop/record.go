package desktop

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/portabledesktop/pd/internal/runtime"
)

// RecordingOptions controls how a desktop recording is started.
type RecordingOptions struct {
	// File is the output path. Defaults to
	// sessionDir/recording-<timestamp>.mp4.
	File string
	// FPS is the capture frame rate. Defaults to 30.
	FPS int
	// IdleSpeedup is the playback acceleration factor for idle
	// segments. Values <= 1 disable idle acceleration.
	IdleSpeedup float64
	// IdleMinDurationSec is the minimum idle segment length before
	// acceleration is applied. Defaults to 0.75.
	IdleMinDurationSec float64
	// IdleNoiseTolerance is the ffmpeg freezedetect noise tolerance.
	// Defaults to "-45dB".
	IdleNoiseTolerance string
}

// idleSpeedupConfig holds validated idle-speedup parameters.
type idleSpeedupConfig struct {
	factor          float64
	minDurationSec  float64
	noiseTolerance  string
}

// RecordingHandle represents an in-progress recording.
type RecordingHandle struct {
	Pid         int
	File        string
	LogPath     string
	cmd         *exec.Cmd
	desktop     *Desktop
	idleConfig  *idleSpeedupConfig
}

// Interval represents a time interval in seconds.
type Interval struct {
	Start float64
	End   float64
}

// StartRecording begins an ffmpeg x11grab recording of the desktop.
func (d *Desktop) StartRecording(opts RecordingOptions) (*RecordingHandle, error) {
	if d.recordingCmd != nil {
		return nil, fmt.Errorf("recording already in progress")
	}

	fps := opts.FPS
	if fps <= 0 {
		fps = 30
	}

	outputPath := opts.File
	if outputPath == "" {
		outputPath = filepath.Join(
			d.SessionDir,
			fmt.Sprintf("recording-%d.mp4", time.Now().UnixMilli()),
		)
	} else {
		var err error
		outputPath, err = filepath.Abs(outputPath)
		if err != nil {
			return nil, fmt.Errorf("resolve output path: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	ffmpegBin := runtime.ResolveRuntimeBinary(d.RuntimeDir, "ffmpeg")

	w, h, err := d.captureSize()
	if err != nil {
		return nil, fmt.Errorf("capture size: %w", err)
	}

	ffmpegArgs := []string{
		"-y",
		"-f", "x11grab",
		"-framerate", strconv.Itoa(fps),
		"-video_size", fmt.Sprintf("%dx%d", w, h),
		"-i", fmt.Sprintf(":%d", d.Display),
		"-codec:v", "libx264",
		"-preset", "ultrafast",
		"-pix_fmt", "yuv420p",
		outputPath,
	}

	logPath := filepath.Join(d.SessionDir, "record.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open record log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(ffmpegBin, ffmpegArgs...)
	cmd.Env = d.Env()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg recorder: %w", err)
	}

	// Brief check for early ffmpeg failure (e.g., bad args, missing
	// display). TS checks within 400ms for spawn errors.
	time.Sleep(400 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		// Process exited. Try to get the exit status.
		waitErr := cmd.Wait()
		if waitErr != nil {
			return nil, fmt.Errorf("ffmpeg recording exited early: %w", waitErr)
		}
		return nil, fmt.Errorf("ffmpeg recording exited immediately")
	}

	d.recordingCmd = cmd

	// Build idle speedup config if applicable.
	var idleCfg *idleSpeedupConfig
	if opts.IdleSpeedup > 1 {
		minDur := opts.IdleMinDurationSec
		if minDur <= 0 {
			minDur = 0.75
		}
		noise := opts.IdleNoiseTolerance
		if noise == "" {
			noise = "-45dB"
		}
		idleCfg = &idleSpeedupConfig{
			factor:         opts.IdleSpeedup,
			minDurationSec: minDur,
			noiseTolerance: noise,
		}
	}

	return &RecordingHandle{
		Pid:        cmd.Process.Pid,
		File:       outputPath,
		LogPath:    logPath,
		cmd:        cmd,
		desktop:    d,
		idleConfig: idleCfg,
	}, nil
}

// Stop gracefully stops the recording (SIGINT), waits for ffmpeg to
// finish, and optionally runs idle-segment speedup.
func (h *RecordingHandle) Stop() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return nil
	}

	// SIGINT tells ffmpeg to finalize the file.
	if err := h.cmd.Process.Signal(syscall.SIGINT); err != nil {
		// Process may have already exited.
		return nil
	}

	// Wait with a timeout.
	done := make(chan error, 1)
	go func() {
		done <- h.cmd.Wait()
	}()

	select {
	case <-done:
		// exited
	case <-time.After(6 * time.Second):
		_ = h.cmd.Process.Kill()
		<-done
	}

	h.desktop.recordingCmd = nil

	if h.idleConfig != nil {
		return h.desktop.speedupIdleSegments(h.File, *h.idleConfig)
	}
	return nil
}

// parseFreezeIntervals extracts freeze intervals from ffmpeg
// freezedetect output. Unterminated freezes extend to durationSec.
// Intervals shorter than 0.02s are filtered out, and overlapping
// intervals (gap ≤ 0.02s) are merged.
func parseFreezeIntervals(output string, durationSec float64) []Interval {
	startRe := regexp.MustCompile(`freeze_start:\s*([0-9.]+)`)
	endRe := regexp.MustCompile(`freeze_end:\s*([0-9.]+)`)

	var intervals []Interval
	var currentStart *float64

	for _, line := range strings.Split(output, "\n") {
		if m := startRe.FindStringSubmatch(line); m != nil {
			v, err := strconv.ParseFloat(m[1], 64)
			if err == nil {
				currentStart = &v
			}
			continue
		}
		if m := endRe.FindStringSubmatch(line); m != nil {
			end, err := strconv.ParseFloat(m[1], 64)
			if err == nil && currentStart != nil && end > *currentStart {
				intervals = append(intervals, Interval{
					Start: math.Max(0, math.Min(*currentStart, durationSec)),
					End:   math.Max(0, math.Min(end, durationSec)),
				})
			}
			currentStart = nil
		}
	}

	// Handle unterminated freeze extending to the end.
	if currentStart != nil && durationSec > *currentStart {
		intervals = append(intervals, Interval{
			Start: math.Max(0, math.Min(*currentStart, durationSec)),
			End:   durationSec,
		})
	}

	if len(intervals) < 2 {
		filtered := make([]Interval, 0, len(intervals))
		for _, iv := range intervals {
			if iv.End-iv.Start > 0.02 {
				filtered = append(filtered, iv)
			}
		}
		return filtered
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start < intervals[j].Start
	})

	var merged []Interval
	for _, iv := range intervals {
		if iv.End-iv.Start <= 0.02 {
			continue
		}
		if len(merged) == 0 || iv.Start > merged[len(merged)-1].End+0.02 {
			merged = append(merged, iv)
		} else {
			merged[len(merged)-1].End = math.Max(merged[len(merged)-1].End, iv.End)
		}
	}

	return merged
}

// speedupIdleSegments runs a two-pass ffmpeg pipeline: first
// freezedetect to find idle intervals, then a filter_complex_script
// to speed them up.
func (d *Desktop) speedupIdleSegments(filePath string, config idleSpeedupConfig) error {
	ffmpegBin := runtime.ResolveRuntimeBinary(d.RuntimeDir, "ffmpeg")

	// Pass 1: freezedetect.
	detectArgs := []string{
		"-hide_banner",
		"-loglevel", "info",
		"-i", filePath,
		"-vf", fmt.Sprintf("freezedetect=n=%s:d=%g", config.noiseTolerance, config.minDurationSec),
		"-an",
		"-f", "null",
		"-",
	}

	detectCmd := exec.Command(ffmpegBin, detectArgs...)
	detectCmd.Env = d.Env()
	detectOut, err := detectCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg freezedetect failed: %w: %s", err, string(detectOut))
	}

	// Get media duration.
	durationSec, err := d.getMediaDurationSeconds(filePath)
	if err != nil {
		return fmt.Errorf("get media duration: %w", err)
	}

	freezeIntervals := parseFreezeIntervals(string(detectOut), durationSec)
	if len(freezeIntervals) == 0 {
		return nil
	}

	// Build segments.
	type segment struct {
		start, end, speed float64
	}
	var segments []segment
	cursor := 0.0

	for _, freeze := range freezeIntervals {
		if freeze.Start > cursor+0.01 {
			segments = append(segments, segment{cursor, freeze.Start, 1})
		}
		if freeze.End > freeze.Start+0.01 {
			segments = append(segments, segment{freeze.Start, freeze.End, config.factor})
		}
		cursor = math.Max(cursor, freeze.End)
	}
	if durationSec > cursor+0.01 {
		segments = append(segments, segment{cursor, durationSec, 1})
	}
	if len(segments) == 0 {
		return nil
	}

	// Build filter_complex script.
	var filterLines []string
	var labels []string
	for i, seg := range segments {
		label := fmt.Sprintf("v%d", i)
		setpts := "PTS-STARTPTS"
		if seg.speed != 1 {
			setpts = fmt.Sprintf("(PTS-STARTPTS)/%g", seg.speed)
		}
		filterLines = append(filterLines,
			fmt.Sprintf("[0:v]trim=start=%f:end=%f,setpts=%s[%s]",
				seg.start, seg.end, setpts, label),
		)
		labels = append(labels, fmt.Sprintf("[%s]", label))
	}
	filterLines = append(filterLines,
		fmt.Sprintf("%sconcat=n=%d:v=1:a=0[vout]",
			strings.Join(labels, ""), len(labels)),
	)

	filterScript := filepath.Join(d.SessionDir,
		fmt.Sprintf("record-speedup-%d.fcs", time.Now().UnixMilli()))
	tempOutput := filePath + ".speedup.tmp.mp4"

	if err := os.WriteFile(filterScript, []byte(strings.Join(filterLines, ";\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write filter script: %w", err)
	}
	defer os.Remove(filterScript)

	renderArgs := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", filePath,
		"-filter_complex_script", filterScript,
		"-map", "[vout]",
		"-an",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		tempOutput,
	}

	renderCmd := exec.Command(ffmpegBin, renderArgs...)
	renderCmd.Env = d.Env()
	renderOut, err := renderCmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tempOutput)
		return fmt.Errorf("ffmpeg speedup render failed: %w: %s", err, string(renderOut))
	}

	if err := os.Rename(tempOutput, filePath); err != nil {
		_ = os.Remove(tempOutput)
		return fmt.Errorf("rename speedup output: %w", err)
	}

	return nil
}

// getMediaDurationSeconds parses the duration from ffmpeg output for
// the given file.
func (d *Desktop) getMediaDurationSeconds(filePath string) (float64, error) {
	ffmpegBin := runtime.ResolveRuntimeBinary(d.RuntimeDir, "ffmpeg")
	cmd := exec.Command(ffmpegBin, "-hide_banner", "-i", filePath, "-f", "null", "-")
	cmd.Env = d.Env()
	out, _ := cmd.CombinedOutput()

	re := regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	m := re.FindStringSubmatch(string(out))
	if m == nil {
		return 0, fmt.Errorf("failed to parse media duration for %s", filePath)
	}

	hours, _ := strconv.Atoi(m[1])
	minutes, _ := strconv.Atoi(m[2])
	seconds, _ := strconv.ParseFloat(m[3], 64)
	total := float64(hours)*3600 + float64(minutes)*60 + seconds
	if total <= 0 {
		return 0, fmt.Errorf("invalid media duration for %s", filePath)
	}
	return total, nil
}
