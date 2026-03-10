package viewer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// Config holds viewer server configuration.
type Config struct {
	VNCHost         string // default "127.0.0.1"
	VNCPort         int
	ListenHost      string // default "127.0.0.1"
	ListenPort      int    // 0 = random
	Scale           string // "fit" or "1:1"
	DesktopSizeMode string // "fixed" or "dynamic"
	AutoOpen        bool   // whether to open browser
	Stdout          io.Writer
}

// viewerConfig is the JSON-serialized configuration embedded
// into the HTML page for the JavaScript client.
type viewerConfig struct {
	Scale           string `json:"scale"`
	DesktopSizeMode string `json:"desktopSizeMode"`
}

// viewerHTML generates the complete HTML page with the viewer
// configuration injected as a global JavaScript variable.
func viewerHTML(config Config) string {
	cfg := viewerConfig{
		Scale:           config.Scale,
		DesktopSizeMode: config.DesktopSizeMode,
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		// viewerConfig only contains simple strings, so
		// marshalling cannot fail in practice.
		cfgJSON = []byte("{}")
	}

	return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>portabledesktop viewer</title>
    <style>
      html, body { margin: 0; width: 100%; height: 100%; background: #12161e; color: #e7ebf3; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif; }
      #topbar { box-sizing: border-box; height: 42px; padding: 10px 14px; border-bottom: 1px solid #2a3342; font-size: 13px; display: flex; align-items: center; }
      #viewer { width: 100%; height: calc(100% - 42px); overflow: hidden; }
    </style>
  </head>
  <body>
    <div id="topbar">connecting...</div>
    <div id="viewer"></div>
    <script>globalThis.PORTABLEDESKTOP_VIEWER_CONFIG = ` + string(cfgJSON) + `;</script>
    <script type="module" src="/viewer.js"></script>
  </body>
</html>`
}

// NewHandler returns an http.Handler that serves the viewer
// static assets (HTML, JS), a health-check endpoint, and the
// WebSocket VNC proxy. Pass a zero-value Config if only testing
// the static routes (the /ws proxy needs VNCHost/VNCPort set).
func NewHandler(config Config) http.Handler {
	mux := http.NewServeMux()

	html := viewerHTML(config)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, html)
	})

	mux.HandleFunc("/viewer.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, viewerClientJS)
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleVNCProxy(w, r, config)
	})

	return mux
}

// Serve starts the HTTP server with a WebSocket-to-VNC proxy
// on /ws. It blocks until ctx is cancelled, then performs a
// graceful shutdown.
func Serve(ctx context.Context, config Config) error {
	if config.VNCHost == "" {
		config.VNCHost = "127.0.0.1"
	}
	if config.ListenHost == "" {
		config.ListenHost = "127.0.0.1"
	}
	if config.Stdout == nil {
		config.Stdout = io.Discard
	}

	handler := NewHandler(config)

	addr := fmt.Sprintf("%s:%d", config.ListenHost, config.ListenPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	actualAddr := ln.Addr().(*net.TCPAddr)
	_, _ = fmt.Fprintf(config.Stdout,
		"viewer: http://%s:%d\n", actualAddr.IP, actualAddr.Port)
	_, _ = fmt.Fprintf(config.Stdout,
		"vnc: %s:%d\n", config.VNCHost, config.VNCPort)

	if config.AutoOpen {
		url := fmt.Sprintf("http://%s:%d",
			actualAddr.IP, actualAddr.Port)
		go openBrowser(url)
	}

	srv := &http.Server{Handler: handler}

	// Run server in a goroutine so we can wait for ctx
	// cancellation.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second)
		defer cancel()
		if shutErr := srv.Shutdown(shutdownCtx); shutErr != nil {
			return fmt.Errorf("shutdown: %w", shutErr)
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// handleVNCProxy upgrades an HTTP connection to a WebSocket
// and bidirectionally proxies traffic to the VNC server.
func handleVNCProxy(
	w http.ResponseWriter,
	r *http.Request,
	config Config,
) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow any origin so the viewer page works from
		// localhost with any port.
		InsecureSkipVerify: true,
	})
	if err != nil {
		// Accept already wrote an HTTP error response.
		return
	}
	defer ws.CloseNow()

	vncAddr := net.JoinHostPort(
		config.VNCHost, fmt.Sprintf("%d", config.VNCPort))
	tcp, err := net.Dial("tcp", vncAddr)
	if err != nil {
		ws.Close(websocket.StatusInternalError,
			"vnc connect failed")
		return
	}
	defer tcp.Close()

	// Wrap the WebSocket in a net.Conn so we can use io.Copy
	// for the bidirectional proxy.
	wsConn := websocket.NetConn(r.Context(), ws, websocket.MessageBinary)

	var wg sync.WaitGroup
	wg.Add(2)

	// TCP → WebSocket.
	go func() {
		defer wg.Done()
		_, _ = io.Copy(wsConn, tcp)
		// Signal the other direction to stop.
		wsConn.Close()
	}()

	// WebSocket → TCP.
	go func() {
		defer wg.Done()
		_, _ = io.Copy(tcp, wsConn)
		// Signal the other direction to stop.
		tcp.Close()
	}()

	wg.Wait()
}

// openBrowser attempts to open the given URL in the user's
// default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	// Best-effort; ignore errors.
	_ = cmd.Start()
}
