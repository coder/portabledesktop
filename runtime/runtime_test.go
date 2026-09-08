package runtime

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestArchive(t *testing.T) {
	t.Parallel()
	linux := runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
	if Available() != linux {
		t.Fatalf("Available() = %v, want %v for %s/%s", Available(), linux, runtime.GOOS, runtime.GOARCH)
	}
	if !linux {
		if ArchiveName != "" || SHA256() != "" || Archive() != nil {
			t.Fatal("no archive expected on this platform")
		}
		return
	}
	if len(SHA256()) != 64 {
		t.Fatalf("SHA256() = %q, want 64 hex chars", SHA256())
	}

	// The archive must decompress and contain the documented entry points.
	zr, err := zstd.NewReader(bytes.NewReader(Archive()))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	want := map[string]bool{
		"./" + XvncPath:                false,
		"./bin/xkbcomp":                false,
		"./" + XKBDir + "/rules/evdev": false,
		"./manifest.json":              false,
	}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := want[hdr.Name]; ok {
			want[hdr.Name] = true
			if hdr.Name == "./"+XvncPath && hdr.Mode&0o111 == 0 {
				t.Fatalf("%s is not executable (mode %o)", hdr.Name, hdr.Mode)
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("archive is missing %s", name)
		}
	}
}
