// Package server is the HTTP layer: JSON API under /api, media under /media and
// the embedded SPA at /.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path"

	"github.com/thisizaro/adbreak/internal/library"
	"github.com/thisizaro/adbreak/internal/manifest"
	"github.com/thisizaro/adbreak/internal/pipeline"
)

type Server struct {
	version string
	static  fs.FS
	lib     *library.Library
	pacing  pipeline.Pacing
	up      *Uploads
}

func New(version string, static fs.FS, lib *library.Library, pacing pipeline.Pacing) *Server {
	return &Server{version: version, static: static, lib: lib, pacing: pacing}
}

// WithUploads enables uploads and async jobs.
func (s *Server) WithUploads(u *Uploads) *Server {
	s.up = u
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/episodes", s.episodes)
	mux.HandleFunc("GET /api/episodes/{id}", s.episode)
	mux.HandleFunc("GET /api/episodes/{id}/vmap.xml", s.vmap)
	mux.HandleFunc("POST /api/episodes/{id}/try-brand", s.tryBrand)
	mux.HandleFunc("GET /api/episodes/{id}/trials/{name}", s.trial)
	mux.HandleFunc("POST /api/uploads", s.createUpload)
	mux.HandleFunc("PUT /api/uploads/{id}", s.putUpload)
	mux.HandleFunc("POST /api/uploads/{id}/finalize", s.finalizeUpload)
	mux.HandleFunc("POST /api/jobs", s.createJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("GET /api/impression", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /media/episodes/{file}", s.video)
	mux.HandleFunc("GET /media/slates/{file}", s.slate)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.Handle("/", s.spa())
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version, "pipeline": pipeline.Version})
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"pacing": s.pacing, "brands": s.lib.Catalogue()})
}

func (s *Server) episodes(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.List()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) episode(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.Result(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) vmap(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.Result(r.PathValue("id"))
	if t := r.URL.Query().Get("trial"); t != "" {
		res, err = s.lib.Trial(r.PathValue("id"), t)
	}
	if err != nil {
		fail(w, err)
		return
	}
	base := baseURL(r)
	var ads []manifest.Ad
	for i, b := range res.Breaks {
		if b.Creative == nil {
			continue
		}
		ads = append(ads, manifest.Ad{
			BreakID: fmt.Sprintf("break-%d", i+1), Offset: b.T, AdID: b.Creative.ID, Title: b.BrandName,
			Seconds: b.Creative.Seconds, MediaURL: base + "/media/slates/" + b.Creative.ID + ".mp4",
			Width: 960, Height: 540, Impression: base + "/api/impression?ad=" + b.Creative.ID,
		})
	}
	out, err := manifest.VMAP(ads)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Write(out)
}

func (s *Server) video(w http.ResponseWriter, r *http.Request) {
	id := trimExt(r.PathValue("file"), ".mp4")
	p, err := s.lib.VideoPath(id)
	if err != nil {
		fail(w, err)
		return
	}
	serveChunked(w, r, p)
}

func (s *Server) slate(w http.ResponseWriter, r *http.Request) {
	p, err := s.lib.SlatePath(r.Context(), trimExt(r.PathValue("file"), ".mp4"))
	if err != nil {
		fail(w, err)
		return
	}
	serveChunked(w, r, p)
}

func trimExt(name, ext string) string {
	if path.Ext(name) == ext {
		return name[:len(name)-len(ext)]
	}
	return "\x00" // never matches a safe id
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, library.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	log.Printf("error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
