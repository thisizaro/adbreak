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
// parseBrand validates a runtime brand from a request body and returns it with
// its trial name. It writes the error response itself and returns ok=false.
func (s *Server) parseBrand(w http.ResponseWriter, r *http.Request) (brands.Brand, string, bool) {
	var b brands.Brand
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read body"})
		return b, "", false
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "brand must be a JSON object like the catalogue entries: " + err.Error()})
		return b, "", false
	}
	if err := b.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return b, "", false
	}
	for _, existing := range s.lib.Catalogue() {
		if existing.ID == b.ID {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("brand_id %q is already in the catalogue", b.ID)})
			return b, "", false
		}
		for _, ec := range existing.Creatives {
			for _, c := range b.Creatives {
				if c.ID == ec.ID {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("creative id %q already belongs to %s; use a new id", c.ID, existing.ID)})
					return b, "", false
				}
			}
		}
	}
	canon, _ := json.Marshal(b)
	return b, fmt.Sprintf("trial_%x", sha256.Sum256(canon))[:18], true
}

func (s *Server) submitBrand(w http.ResponseWriter, kind, episode string, b brands.Brand, trial string) {
	payload, _ := json.Marshal(map[string]any{"brand": b, "trial": trial})
	s.lib.AddBrand(b)
	j, err := s.up.Runner.SubmitKind(kind, episode, payload)
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

// tryBrand queues a job that re-places one analysed episode's breaks with one
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
	if b, trial, ok := s.parseBrand(w, r); ok {
		s.submitBrand(w, "try-brand", id, b, trial)
	}
}

// tryBrandAll queues one job that re-places every analysed episode.
func (s *Server) tryBrandAll(w http.ResponseWriter, r *http.Request) {
	if s.up == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if b, trial, ok := s.parseBrand(w, r); ok {
		s.submitBrand(w, "try-brand-all", "*", b, trial)
	}
}

func (s *Server) trialAll(w http.ResponseWriter, r *http.Request) {
	out, err := s.lib.TrialAll(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) trial(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.Trial(r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
