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

// TrialBreak is one scheduled break in a try-a-brand result, with how the new brand fared there.
type TrialBreak struct {
	T         float64 `json:"t"`
	Winner    string  `json:"winner"`
	WinnerFit float64 `json:"winner_fit"`
	NewFit    float64 `json:"new_brand_fit"`
	Outcome   string  `json:"outcome"` // "won", "lost", "blocked", "unfit"
	Reason    string  `json:"reason"`
}

type EpisodeTrial struct {
	Episode string       `json:"episode"`
	Breaks  []TrialBreak `json:"breaks"`
	Won     int          `json:"won"`
}

// Summarize explains, break by break, how the trial brand did in one result.
func Summarize(r pipeline.Result) EpisodeTrial {
	et := EpisodeTrial{Episode: r.Episode, Breaks: []TrialBreak{}}
	nb := r.TrialBrand
	for _, b := range r.Breaks {
		d := b.Placement.Decision
		tb := TrialBreak{T: b.T, Winner: b.BrandName, WinnerFit: d.Fit}
		for _, v := range b.Placement.Verdicts {
			if v.BrandID == nb {
				tb.NewFit = v.Fit
			}
		}
		switch {
		case d.BrandID == nb:
			tb.Outcome, tb.Reason = "won", d.Rationale
			et.Won++
		case d.Blocked[nb] != "":
			tb.Outcome, tb.Reason = "blocked", d.Blocked[nb]
		case d.Unfit[nb] != "":
			tb.Outcome, tb.Reason = "unfit", d.Unfit[nb]
		default:
			tb.Outcome, tb.Reason = "lost", fmt.Sprintf("%s fits better (%.2f vs %.2f)", b.BrandName, d.Fit, tb.NewFit)
		}
		et.Breaks = append(et.Breaks, tb)
	}
	kept := map[float64]bool{}
	for _, t := range r.TrialKept {
		kept[t] = true
	}
	for _, u := range r.Unplaced {
		if !kept[u.T] {
			continue
		}
		tb := TrialBreak{T: u.T, Winner: "none", Outcome: "unfit", Reason: "no brand placed here in the re-match"}
		for _, v := range u.Verdicts {
			if v.BrandID == nb {
				tb.NewFit = v.Fit
			}
		}
		if why := u.Decision.Blocked[nb]; why != "" {
			tb.Outcome, tb.Reason = "blocked", why
		} else if why := u.Decision.Unfit[nb]; why != "" {
			tb.Reason = why
		}
		et.Breaks = append(et.Breaks, tb)
	}
	sort.Slice(et.Breaks, func(i, j int) bool { return et.Breaks[i].T < et.Breaks[j].T })
	return et
}

// TrialAll summarizes one trial across every episode that has it.
func (l *Library) TrialAll(name string) ([]EpisodeTrial, error) {
	if !trialName.MatchString(name) {
		return nil, ErrNotFound
	}
	list, err := l.List()
	if err != nil {
		return nil, err
	}
	out := []EpisodeTrial{}
	for _, s := range list {
		if r, err := l.Trial(s.ID, name); err == nil {
			out = append(out, Summarize(r))
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

var uploadedID = regexp.MustCompile(`^up_[0-9]+$`)

// ErrNotDeletable is returned for anything that is not an uploaded video.
var ErrNotDeletable = errors.New("only uploaded videos can be deleted")

// DeleteUpload permanently removes an uploaded video and every cached
// analysis artifact for it. Sample episodes cannot be deleted.
func (l *Library) DeleteUpload(id string) error {
	if !uploadedID.MatchString(id) {
		return ErrNotDeletable
	}
	video := filepath.Join(l.VideoDir, id+".mp4")
	dir := filepath.Join(l.DataDir, id)
	_, vErr := os.Stat(video)
	_, dErr := os.Stat(dir)
	if vErr != nil && dErr != nil {
		return ErrNotFound
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.Remove(video); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
