package reporting

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/coder/portabledesktop/examples/agent-go/clickprobe"
	"github.com/coder/portabledesktop/examples/agent-go/display"
)

type AgentMetrics struct {
	ToolCalls   int
	MouseMoves  int
	Clicks      int
	Screenshots int
	Waits       int
}

type Summary struct {
	Model         string
	MaxSteps      int
	RecordingPath string
	MessagesPath  string
	Geometry      display.Geometry
	Clickprobe    *clickprobe.Summary
	AgentMetrics  AgentMetrics
	RunErr        error
}

func FormatAction(a anthropic.ComputerUseInput) string {
	marshalled, err := json.Marshal(a)
	if err != nil {
		return fmt.Sprintf("error marshalling action: %v", err)
	}
	return string(marshalled)
}

func SaveMessages(path string, messages fantasy.Prompt) {
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to marshal messages: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write messages: %v\n", err)
	}
}

func PrintSummary(summary Summary) {
	fmt.Println("\npost-run summary:")
	fmt.Printf("  model: %s\n", summary.Model)
	fmt.Printf("  max steps: %d\n", summary.MaxSteps)
	fmt.Printf("  recording path: %s\n", summary.RecordingPath)
	fmt.Printf("  messages path: %s\n", summary.MessagesPath)
	if !summary.Geometry.IsZero() {
		fmt.Printf("  native geometry: %dx%d\n", summary.Geometry.NativeWidth, summary.Geometry.NativeHeight)
		fmt.Printf("  declared geometry: %dx%d\n", summary.Geometry.DeclaredWidth, summary.Geometry.DeclaredHeight)
	}

	fmt.Printf("  agent tool calls: %d\n", summary.AgentMetrics.ToolCalls)
	fmt.Printf("  agent mouse moves: %d\n", summary.AgentMetrics.MouseMoves)
	fmt.Printf("  agent-issued clicks: %d\n", summary.AgentMetrics.Clicks)
	fmt.Printf("  screenshots returned: %d\n", summary.AgentMetrics.Screenshots)
	fmt.Printf("  wait actions: %d\n", summary.AgentMetrics.Waits)

	if summary.Clickprobe != nil {
		clickprobe.PrintSummary(summary.Clickprobe, summary.AgentMetrics.Clicks)
	}

	if summary.RunErr != nil {
		fmt.Printf("  run error: %v\n", summary.RunErr)
	}
}

func boolString(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
