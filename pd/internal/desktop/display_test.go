package desktop

import (
	"net"
	"testing"
)

func TestPickDisplayAndPort_FirstFree(t *testing.T) {
	display, port, err := PickDisplayAndPort(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if display < 1 || display > 99 {
		t.Fatalf("expected display in [1,99], got %d", display)
	}
	if port != 5900+display {
		t.Fatalf("expected port=%d, got %d", 5900+display, port)
	}
}

func TestPickDisplayAndPort_SkipsOccupied(t *testing.T) {
	// Occupy port 5901 so display=1 should be skipped.
	ln, err := net.Listen("tcp", "127.0.0.1:5901")
	if err != nil {
		t.Skipf("cannot bind 5901, skipping: %v", err)
	}
	defer ln.Close()

	display, port, err := PickDisplayAndPort(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// It should not have returned display=1 since port 5901 is
	// occupied.
	if port == 5901 {
		t.Fatalf("expected to skip occupied port 5901, got display=%d port=%d", display, port)
	}
	if port != 5900+display {
		t.Fatalf("expected port=%d for display=%d, got %d", 5900+display, display, port)
	}
}

func TestPickDisplayAndPort_ExplicitValues(t *testing.T) {
	d := 5
	p := 6000
	display, port, err := PickDisplayAndPort(&d, &p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if display != 5 {
		t.Fatalf("expected display=5, got %d", display)
	}
	if port != 6000 {
		t.Fatalf("expected port=6000, got %d", port)
	}
}
