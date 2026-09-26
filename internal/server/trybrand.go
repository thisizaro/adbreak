package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/jobs"
)

// tryBrand queues a job that re-places an analysed episode's breaks with one
// extra brand added to the catalogue. Break positions do not change.
func (s *Server) tryBrand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.up == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if _, err := s.lib.Result(id); err != nil {
		fail(w, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read body"})
		return
	}
	var b brands.Brand
	if err := json.Unmarshal(raw, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand must be a JSON object like the catalogue entries: " + err.Error()})
		return
	}
	if err := b.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for _, existing := range s.lib.Catalogue() {
		if existing.ID == b.ID {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("brand_id %q is already in the catalogue", b.ID)})
			return
		}
	}
	canon, _ := json.Marshal(b)
	payload, _ := json.Marshal(map[string]any{"brand": b, "trial": fmt.Sprintf("trial_%x", sha256.Sum256(canon))[:18]})
	s.lib.AddBrand(b)
	j, err := s.up.Runner.SubmitKind("try-brand", id, payload)
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

func (s *Server) trial(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.Trial(r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
