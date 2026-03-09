package viewer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewerHandler_RootReturnsHTML(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		Scale:           "fit",
		DesktopSizeMode: "fixed",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(),
		"<title>portabledesktop viewer</title>")
}

func TestViewerHandler_ViewerJSReturnsJS(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{})

	req := httptest.NewRequest(http.MethodGet, "/viewer.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"),
		"text/javascript")
}

func TestViewerHandler_HealthzReturnsOk(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	body := strings.TrimSpace(rec.Body.String())
	require.Equal(t, "ok", body)
}

func TestViewerHandler_UnknownPathReturns404(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
