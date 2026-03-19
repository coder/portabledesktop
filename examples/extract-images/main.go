// extract-images reads a Fantasy SDK conversation JSON file and saves
// all embedded images (from tool-result media outputs and file parts)
// to a directory on disk.
//
// Usage:
//
//	go run main.go <conversation.json> <output-dir>
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// messagePart is the envelope used for each element in a message's
// content array: {"type": "...", "data": {...}}.
type messagePart struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// toolResultData holds the fields we care about inside a tool-result part.
type toolResultData struct {
	ToolCallID string          `json:"tool_call_id"`
	Output     json.RawMessage `json:"output"`
}

// toolResultOutput is the envelope for the output field inside a
// tool-result: {"type": "media", "data": {...}}.
type toolResultOutput struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// mediaData holds the base64-encoded payload and its MIME type.
type mediaData struct {
	Data      string `json:"data"`
	MediaType string `json:"media_type"`
}

// filePartData holds a file part's payload.
type filePartData struct {
	Filename  string `json:"filename"`
	Data      string `json:"data"`
	MediaType string `json:"media_type"`
}

// message mirrors fantasy.Message just enough to walk the content.
type message struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

// extractedImage is a decoded image ready to be written to disk.
type extractedImage struct {
	data      []byte
	mediaType string
	label     string // descriptive label for the filename
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <conversation.json> <output-dir>\n", os.Args[0])
		os.Exit(1)
	}
	convPath := os.Args[1]
	outDir := os.Args[2]

	raw, err := os.ReadFile(convPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", convPath, err)
		os.Exit(1)
	}

	var messages []message
	if err := json.Unmarshal(raw, &messages); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse conversation: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: create output dir: %v\n", err)
		os.Exit(1)
	}

	var images []extractedImage
	for mi, msg := range messages {
		for pi, rawPart := range msg.Content {
			var part messagePart
			if err := json.Unmarshal(rawPart, &part); err != nil {
				continue
			}

			switch part.Type {
			case "tool-result":
				img := extractToolResultMedia(part.Data)
				if img != nil {
					img.label = fmt.Sprintf("msg%03d_part%03d_tool_result", mi, pi)
					images = append(images, *img)
				}
			case "file":
				img := extractFilePart(part.Data)
				if img != nil {
					img.label = fmt.Sprintf("msg%03d_part%03d_file", mi, pi)
					images = append(images, *img)
				}
			}
		}
	}

	if len(images) == 0 {
		fmt.Println("no images found")
		return
	}

	for i, img := range images {
		ext := extForMediaType(img.mediaType)
		filename := fmt.Sprintf("%04d_%s%s", i+1, img.label, ext)
		path := filepath.Join(outDir, filename)
		if err := os.WriteFile(path, img.data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write %s: %v\n", path, err)
			continue
		}
		fmt.Printf("wrote %s (%d bytes, %s)\n", path, len(img.data), img.mediaType)
	}
	fmt.Printf("\nextracted %d image(s)\n", len(images))
}

// extractToolResultMedia pulls image data from a tool-result part's
// media output, if present.
func extractToolResultMedia(data json.RawMessage) *extractedImage {
	var tr toolResultData
	if err := json.Unmarshal(data, &tr); err != nil || len(tr.Output) == 0 {
		return nil
	}

	var out toolResultOutput
	if err := json.Unmarshal(tr.Output, &out); err != nil {
		return nil
	}
	if out.Type != "media" {
		return nil
	}

	var md mediaData
	if err := json.Unmarshal(out.Data, &md); err != nil {
		return nil
	}
	if !isImageMediaType(md.MediaType) {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(md.Data)
	if err != nil {
		// Try URL-safe or raw variants.
		decoded, err = base64.RawStdEncoding.DecodeString(md.Data)
		if err != nil {
			return nil
		}
	}
	return &extractedImage{data: decoded, mediaType: md.MediaType}
}

// extractFilePart pulls image data from a file part.
func extractFilePart(data json.RawMessage) *extractedImage {
	var fp filePartData
	if err := json.Unmarshal(data, &fp); err != nil {
		return nil
	}
	if !isImageMediaType(fp.MediaType) {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(fp.Data)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(fp.Data)
		if err != nil {
			return nil
		}
	}
	return &extractedImage{data: decoded, mediaType: fp.MediaType}
}

func isImageMediaType(mt string) bool {
	return strings.HasPrefix(mt, "image/")
}

func extForMediaType(mt string) string {
	// mime.ExtensionsByType can return multiple; pick the most common.
	exts, _ := mime.ExtensionsByType(mt)
	if len(exts) > 0 {
		// Prefer .png, .jpg, .jpeg, .webp over obscure alternatives.
		preferred := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".svg": true}
		for _, e := range exts {
			if preferred[e] {
				return e
			}
		}
		return exts[0]
	}
	// Fallback based on common types.
	switch mt {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".bin"
	}
}
