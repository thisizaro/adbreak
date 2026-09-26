package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/thisizaro/adbreak/internal/jobs"
	"github.com/thisizaro/adbreak/internal/media"
)

// Uploads follow the production flow (get an upload URL, PUT bytes there, then
// start a job). Locally the URL is this server; in production the blob seam
// hands out a GCS signed URL instead, because Cloud Run caps request bodies at 32 MiB.
type Uploads struct {
	Target      UploadTarget
	VideoDir    string
	MaxBytes    int64
	MaxDuration float64
	Runner      *jobs.Runner
}

var uploadID = regexp.MustCompile(`^up_[0-9]+$`)

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	if s.up == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "uploads disabled"})
		return
	}
	var req struct {
		Size int64 `json:"size"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req)
	if s.up.Runner.CapReached() {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": jobs.ErrDailyCap.Error()})
		return
	}
	if req.Size > s.up.MaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("file is %d MB, limit is %d MB", req.Size>>20, s.up.MaxBytes>>20)})
		return
	}
	id := fmt.Sprintf("up_%d", time.Now().UnixNano())
	target, err := s.up.Target.Begin(r.Context(), id, baseURL(r), req.Size)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "upload_url": target})
}

// finalizeUpload validates an uploaded file wherever it was PUT (this server or GCS).
func (s *Server) finalizeUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uploadID.MatchString(id) || s.up == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	path := filepath.Join(s.up.VideoDir, id+".mp4")
	st, err := os.Stat(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "upload not found"})
		return
	}
	reject := func(msg string) {
		os.Remove(path)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": msg})
	}
	if st.Size() > s.up.MaxBytes {
		reject(fmt.Sprintf("file is %d MB, limit is %d MB", st.Size()>>20, s.up.MaxBytes>>20))
		return
	}
	info, err := media.Probe(r.Context(), path)
	if err != nil || !info.HasAudio {
		reject("not a video with an audio track")
		return
	}
	if info.Duration > s.up.MaxDuration {
		reject(fmt.Sprintf("video is %.0fs, limit is %.0fs", info.Duration, s.up.MaxDuration))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "bytes": st.Size(), "duration": info.Duration})
}

func (s *Server) putUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, local := s.up.Target.(LocalTarget); !uploadID.MatchString(id) || s.up == nil || !local {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	dst := filepath.Join(s.up.VideoDir, id+".mp4")
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, s.up.MaxBytes))
	f.Close()
	if err != nil {
		os.Remove(tmp)
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("upload failed after %d bytes: %v", n, err)})
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "bytes": n})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Episode string `json:"episode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || s.up == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"episode\": \"<id>\"}"})
		return
	}
	if _, err := s.lib.VideoPath(req.Episode); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown episode"})
		return
	}
	j, err := s.up.Runner.Submit(req.Episode)
	if errors.Is(err, jobs.ErrDailyCap) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, j)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	if s.up == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	j, ok := s.up.Runner.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, j)
}
