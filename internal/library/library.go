// Package library is the read side: which episodes have results, where their
// video lives, and on-demand slate rendering. Local disk today; the blob seam
// (GCS) replaces the paths in production.
package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/pipeline"
	"github.com/thisizaro/adbreak/internal/slates"
)

var ErrNotFound = errors.New("not found")

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type Library struct {
	DataDir   string
	VideoDir  string
	Catalogue func() []brands.Brand
	Font      string
	mu        sync.Mutex
	extraMu   sync.RWMutex
	extra     map[string]brands.Brand
}

// AddBrand registers a runtime brand so its slates can be rendered.
func (l *Library) AddBrand(b brands.Brand) {
	l.extraMu.Lock()
	defer l.extraMu.Unlock()
	if l.extra == nil {
		l.extra = map[string]brands.Brand{}
	}
	l.extra[b.ID] = b
}

func (l *Library) allBrands() []brands.Brand {
	out := append([]brands.Brand(nil), l.Catalogue()...)
	l.extraMu.RLock()
	defer l.extraMu.RUnlock()
	for _, b := range l.extra {
		out = append(out, b)
	}
	return out
}

var trialName = regexp.MustCompile(`^trial_[0-9a-f]{12}$`)

// Trial loads a try-a-brand result for an episode.
func (l *Library) Trial(id, name string) (pipeline.Result, error) {
	var r pipeline.Result
	if !safeID.MatchString(id) || !trialName.MatchString(name) {
		return r, ErrNotFound
	}
	b, err := os.ReadFile(filepath.Join(l.DataDir, id, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

type Summary struct {
	ID         string          `json:"id"`
	Duration   float64         `json:"duration"`
	Breaks     int             `json:"breaks"`
	Version    string          `json:"pipeline_version"`
	ComputedAt string          `json:"computed_at"`
	Funnel     pipeline.Funnel `json:"funnel"`
	BreakTimes []float64       `json:"break_times"`
	Suppressed []float64       `json:"suppressed_times"`
	Brands     []string        `json:"brands"`
}

func (l *Library) List() ([]Summary, error) {
	entries, err := os.ReadDir(l.DataDir)
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := l.Result(e.Name())
		if err != nil {
			continue
		}
		sum := Summary{ID: r.Episode, Duration: r.Media.Duration, Breaks: len(r.Breaks), Version: r.Version,
			ComputedAt: r.ComputedAt.Format("2006-01-02 15:04 MST"), Funnel: r.Funnel,
			BreakTimes: []float64{}, Suppressed: []float64{}, Brands: []string{}}
		for _, b := range r.Breaks {
			sum.BreakTimes = append(sum.BreakTimes, b.T)
			sum.Brands = append(sum.Brands, b.BrandName)
		}
		for _, u := range r.Unplaced {
			sum.Suppressed = append(sum.Suppressed, u.T)
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (l *Library) Result(id string) (pipeline.Result, error) {
	var r pipeline.Result
	if !safeID.MatchString(id) {
		return r, ErrNotFound
	}
	b, err := os.ReadFile(filepath.Join(l.DataDir, id, "debug.json"))
	if errors.Is(err, os.ErrNotExist) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

func (l *Library) VideoPath(id string) (string, error) {
	if !safeID.MatchString(id) {
		return "", ErrNotFound
	}
	p := filepath.Join(l.VideoDir, id+".mp4")
	if _, err := os.Stat(p); err != nil {
		return "", ErrNotFound
	}
	return p, nil
}

// SlatePath returns a rendered slate for a creative in the current catalogue,
// rendering it on first request.
func (l *Library) SlatePath(ctx context.Context, creativeID string) (string, error) {
	if !safeID.MatchString(creativeID) {
		return "", ErrNotFound
	}
	for _, b := range l.allBrands() {
		for _, c := range b.Creatives {
			if c.ID != creativeID {
				continue
			}
			dir := filepath.Join(l.DataDir, "_slates")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
			out := filepath.Join(dir, creativeID+".mp4")
			l.mu.Lock()
			defer l.mu.Unlock()
			err := slates.Render(ctx, slates.Spec{BrandName: b.Name, Category: b.Category, Seconds: c.Seconds,
				Width: 960, Height: 540, Font: l.Font}, out)
			if err != nil {
				return "", fmt.Errorf("slate %s: %w", creativeID, err)
			}
			return out, nil
		}
	}
	return "", ErrNotFound
}
