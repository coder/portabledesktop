package desktop

import (
	"fmt"
	"net"
	"os"
)

// PickDisplayAndPort selects a free X display number and VNC port.
//
// When both requestedDisplay and requestedPort are provided they are
// returned directly. When only one is given the other is derived
// (port = 5900 + display, or display = port − 5900 when the port
// falls in 5900–5999). When neither is provided the function scans
// display 1 through 99 looking for a pair where the X11 socket does
// not exist and the TCP port is free.
func PickDisplayAndPort(requestedDisplay, requestedPort *int) (display int, port int, err error) {
	if requestedDisplay != nil && requestedPort != nil {
		return *requestedDisplay, *requestedPort, nil
	}

	if requestedDisplay != nil {
		d := *requestedDisplay
		return d, 5900 + d, nil
	}

	if requestedPort != nil {
		p := *requestedPort
		d := 1
		if p >= 5900 && p <= 5999 {
			d = p - 5900
		}
		return d, p, nil
	}

	// Scan for the first available display+port pair.
	for d := 1; d <= 99; d++ {
		p := 5900 + d

		// Skip if the X11 socket already exists.
		socketPath := fmt.Sprintf("/tmp/.X11-unix/X%d", d)
		if _, err := os.Stat(socketPath); err == nil {
			continue
		}

		// Try binding the TCP port to see if it is free.
		ln, listenErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if listenErr != nil {
			continue
		}
		ln.Close()
		return d, p, nil
	}

	return 0, 0, fmt.Errorf("no available display/port pairs in :1-:99 / 5901-5999")
}
