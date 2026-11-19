// Package web serves the embedded Admin Web console and the Provider HTTP API
// on one standard-library HTTP server.
//
// The server uses net/http directly (instead of a fasthttp bridge) so that
// streaming responses such as the technical-log SSE endpoint are flushed
// incrementally and their request context is cancelled when the browser
// disconnects or the server shuts down.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed assets/*
var assets embed.FS

var staticAssets = map[string]string{
	"/assets/app.css":       "text/css; charset=utf-8",
	"/assets/app.js":        "text/javascript; charset=utf-8",
	"/assets/log-stream.js": "text/javascript; charset=utf-8",
}

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// NewHandler returns the console and API router. When enabled is false the
// console is not served but the API remains available.
func NewHandler(api http.Handler, enabled bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		path := request.URL.Path
		if api != nil && (path == "/api" || strings.HasPrefix(path, "/api/") || path == "/metrics") {
			api.ServeHTTP(writer, request)
			return
		}
		if enabled && (request.Method == http.MethodGet || request.Method == http.MethodHead) {
			switch {
			case path == "/":
				serveAsset(writer, "assets/index.html", "text/html; charset=utf-8", "no-store")
				return
			case path == "/favicon.ico":
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			if contentType, ok := staticAssets[path]; ok {
				// Assets are embedded in the binary: they load without Internet
				// access. no-cache keeps them consistent with the running build.
				serveAsset(writer, strings.TrimPrefix(path, "/"), contentType, "no-cache")
				return
			}
		}
		header.Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": "route not found"})
	})
}

func serveAsset(writer http.ResponseWriter, name, contentType, cacheControl string) {
	data, err := assets.ReadFile(name)
	if err != nil {
		http.Error(writer, "asset unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", cacheControl)
	_, _ = writer.Write(data)
}

// Server hosts the handler with conservative timeouts. Streaming handlers
// extend their own write deadline through http.ResponseController.
type Server struct {
	server *http.Server
	cancel context.CancelFunc
	once   sync.Once
}

func NewServer(api http.Handler, enabled bool) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cancel: cancel,
		server: &http.Server{
			Handler:           NewHandler(api, enabled),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    64 << 10,
			// Cancelled on shutdown so that long-lived streams end promptly.
			BaseContext: func(net.Listener) context.Context { return ctx },
		},
	}
}

// Listener serves on an existing listener until shutdown.
func (server *Server) Listener(listener net.Listener) error {
	err := server.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ShutdownWithContext cancels open streams and drains in-flight requests.
func (server *Server) ShutdownWithContext(ctx context.Context) error {
	server.once.Do(server.cancel)
	return server.server.Shutdown(ctx)
}
