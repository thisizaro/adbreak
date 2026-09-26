// Package server is the HTTP layer: JSON API under /api and the embedded SPA at /.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
)

type Server struct {
	version string
	static  fs.FS
}

func New(version string, static fs.FS) *Server {
	return &Server{version: version, static: static}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.Handle("/", s.spa())
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

// spa serves embedded files, falling back to index.html for client-side routes.
func (s *Server) spa() http.Handler {
	files := http.FileServerFS(s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(r.URL.Path)[1:]
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(s.static, name); errors.Is(err, fs.ErrNotExist) {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
