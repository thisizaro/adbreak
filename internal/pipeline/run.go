package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/breaks"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/scenes"
)

// Version is stamped on every result so the UI can show which pipeline produced it.
const Version = "0.1.0"

type Pacing struct {
	MaxBreaksPerHour float64 `json:"max_breaks_per_hour"`
	MinGap           float64 `json:"min_gap_sec"`
	MaxAdLoadPct     float64 `json:"max_ad_load_pct"`
	HeadMargin       float64 `json:"head_margin_sec"`
	TailMargin       float64 `json:"tail_margin_sec"`
	SpeechMargin     float64 `json:"speech_margin_sec"`
	PodSeconds       float64 `json:"pod_seconds"`
	MinScore         float64 `json:"min_score"`
}

type Funnel struct {
	Shots          int `json:"shots"`
	Scenes         int `json:"scenes"`
	Candidates     int `json:"candidates"`
	PassHardFilter int `json:"pass_hard_filters"`
	PassAIJudge    int `json:"pass_ai_speech_check"`
	BreakBudget    int `json:"break_budget"`
	Selected       int `json:"selected"`
	Placed         int `json:"placed"`
}

type Break struct {
	T         float64          `json:"t"`
	Score     float64          `json:"score"`
	Rationale string           `json:"rationale"`
	Placement Placement        `json:"placement"`
	Creative  *brands.Creative `json:"creative,omitempty"`
	BrandName string           `json:"brand_name,omitempty"`
}

// Result is the debug JSON: everything chosen and everything rejected, with reasons.
type Result struct {
	Episode     string             `json:"episode"`
	Version     string             `json:"pipeline_version"`
	ComputedAt  time.Time          `json:"computed_at"`
	Media       media.Info         `json:"media"`
	Scenes      []scenes.Scene     `json:"scenes"`
	Pacing      Pacing             `json:"pacing"`
	Funnel      Funnel             `json:"funnel"`
	Breaks      []Break            `json:"breaks"`
	Unplaced    []Placement        `json:"unplaced,omitempty"`
	Candidates  []breaks.Candidate `json:"candidates"`
	ASRProvider string             `json:"asr_provider"`
	AIProvider  string             `json:"ai_provider"`
}

func (d Deps) Run(ctx context.Context, ep Episode, catalogue []brands.Brand, p Pacing) (Result, error) {
	m, err := d.Media(ctx, ep)
	if err != nil {
		return Result{}, err
	}
	tr, err := d.Transcript(ctx, ep)
	if err != nil {
		return Result{}, err
	}
	r := Result{Episode: ep.ID, Version: Version, ComputedAt: time.Now().UTC(), Media: m.Info, Pacing: p,
		ASRProvider: d.ASR.Name(), AIProvider: d.AI.Name()}
	r.Funnel.Shots = len(m.Shots) + 1
	sc, err := d.Scenes(ctx, ep, m, tr)
	if err != nil {
		return Result{}, err
	}
	r.Scenes = sc
	r.Funnel.Scenes = len(sc)

	var cands []breaks.Candidate
	for _, t := range m.Shots {
		cands = append(cands, breaks.Candidate{T: t, Signals: []string{"shot_cut"}})
	}
	r.Funnel.Candidates = len(cands)
	cands = breaks.Filter(cands, tr, breaks.Rules{Duration: m.Info.Duration, HeadMargin: p.HeadMargin,
		TailMargin: p.TailMargin, SpeechMargin: p.SpeechMargin})
	r.Funnel.PassHardFilter = countLive(cands)
	d.progress("filter", "%d of %d candidates pass hard filters", r.Funnel.PassHardFilter, len(cands))

	cands, err = d.Judge(ctx, ep, cands, tr)
	if err != nil {
		return Result{}, err
	}
	r.Funnel.PassAIJudge = countLive(cands)
	var sceneStarts []float64
	for _, s := range sc[1:] {
		sceneStarts = append(sceneStarts, s.Start)
	}
	cands = breaks.ApplySceneContext(cands, sceneStarts)

	budget := breaks.MaxBreaks(m.Info.Duration, p.MaxBreaksPerHour, p.MaxAdLoadPct, p.PodSeconds)
	r.Funnel.BreakBudget = budget
	picked := breaks.Select(cands, breaks.Limits{MaxBreaks: budget, MinGap: p.MinGap, MinScore: p.MinScore})
	r.Funnel.Selected = len(picked)

	byID := map[string]brands.Brand{}
	for _, b := range catalogue {
		byID[b.ID] = b
	}
	for _, i := range picked {
		c := cands[i]
		pl, err := d.Place(ctx, ep, c.T, catalogue, tr, p.PodSeconds)
		if err != nil {
			return Result{}, err
		}
		if pl.Decision.BrandID == "" {
			r.Unplaced = append(r.Unplaced, pl)
			continue
		}
		b := byID[pl.Decision.BrandID]
		br := Break{T: c.T, Score: c.Score, Rationale: c.Rationale, Placement: pl, BrandName: b.Name}
		for _, cr := range b.Creatives {
			if cr.ID == pl.Decision.CreativeID {
				cr := cr
				br.Creative = &cr
			}
		}
		r.Breaks = append(r.Breaks, br)
	}
	r.Funnel.Placed = len(r.Breaks)
	r.Candidates = cands

	b, _ := json.MarshalIndent(r, "", "  ")
	return r, os.WriteFile(filepath.Join(ep.Dir, "debug.json"), b, 0o644)
}

func countLive(cs []breaks.Candidate) int {
	n := 0
	for _, c := range cs {
		if c.Rejected == "" {
			n++
		}
	}
	return n
}
