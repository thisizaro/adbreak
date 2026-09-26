package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/breaks"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/scenes"
	"github.com/thisizaro/adbreak/internal/speech"
)

// RejectNoBrand marks a selected break that no brand could take.
const RejectNoBrand = "no eligible brand (all blocked or no fit)"

// Version is stamped on every result so the UI can show which pipeline produced it.
const Version = "0.3.0"

type Pacing struct {
	MaxBreaksPerHour float64 `json:"max_breaks_per_hour"`
	MinGap           float64 `json:"min_gap_sec"`
	MaxAdLoadPct     float64 `json:"max_ad_load_pct"`
	HeadMargin       float64 `json:"head_margin_sec"`
	TailMargin       float64 `json:"tail_margin_sec"`
	SpeechMargin     float64 `json:"speech_margin_sec"`
	PodSeconds       float64 `json:"pod_seconds"`
	MinScore         float64 `json:"min_score"`
	HeadPct          float64 `json:"head_pct"`
	TailPct          float64 `json:"tail_pct"`
	GapFraction      float64 `json:"gap_fraction"`
}

// Effective is the pacing actually applied to this episode after scaling.
type Effective struct {
	Head   float64 `json:"head_margin_sec"`
	Tail   float64 `json:"tail_margin_sec"`
	MinGap float64 `json:"min_gap_sec"`
	Budget int     `json:"break_budget"`
}

// Boundary explains how a break relates to the scene structure.
type Boundary struct {
	SceneIndex int     `json:"scene_index"`
	SceneStart float64 `json:"scene_start"`
	Shift      float64 `json:"shift_sec"`
	Note       string  `json:"note"`
}

type Funnel struct {
	Shots             int `json:"shots"`
	Scenes            int `json:"scenes"`
	Candidates        int `json:"candidates"`
	PassHardFilter    int `json:"pass_hard_filters"`
	PassAIJudge       int `json:"pass_ai_speech_check"`
	CaughtByAudio     int `json:"caught_by_ai_audio_check"` // cleared by Whisper, rejected by the AI audio check
	BreakBudget       int `json:"break_budget"`
	Considered        int `json:"considered_for_placement"` // = placed + suppressed
	Placed            int `json:"placed"`
	Suppressed        int `json:"suppressed_for_brand_safety"`    // no brand could take it (blocked or no fit)
	FinalCheckRejects int `json:"rejected_by_final_speech_check"` // selected, then failed the per-break speech re-check
}

type Break struct {
	T         float64          `json:"t"`
	Score     float64          `json:"score"`
	Rationale string           `json:"rationale"`
	Placement Placement        `json:"placement"`
	Boundary  Boundary         `json:"boundary"`
	Safety    []SafetyCheck    `json:"safety_checks"`
	Flags     brands.Flags     `json:"independent_flags"`
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
	Effective   Effective          `json:"effective_pacing"`
	Funnel      Funnel             `json:"funnel"`
	Breaks      []Break            `json:"breaks"`
	Unplaced    []Placement        `json:"unplaced"`
	Candidates  []breaks.Candidate `json:"candidates"`
	ASRProvider string             `json:"asr_provider"`
	AIProvider  string             `json:"ai_provider"`
	DecideModel string             `json:"decide_provider,omitempty"`
	Trial       string             `json:"trial,omitempty"`
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
	if d.Decide != nil {
		r.DecideModel = d.Decide.Name()
	}
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
	sp := breaks.Scale(m.Info.Duration, p.HeadMargin, p.HeadPct, p.TailMargin, p.TailPct, p.MinGap, p.GapFraction)
	r.Effective = Effective{Head: sp.Head, Tail: sp.Tail, MinGap: sp.MinGap}
	cands = breaks.Filter(cands, tr, breaks.Rules{Duration: m.Info.Duration, HeadMargin: sp.Head,
		TailMargin: sp.Tail, SpeechMargin: p.SpeechMargin})
	r.Funnel.PassHardFilter = countLive(cands)
	d.progress("filter", "%d of %d candidates pass hard filters", r.Funnel.PassHardFilter, len(cands))

	cands, err = d.Judge(ctx, ep, cands, tr)
	if err != nil {
		return Result{}, err
	}
	r.Funnel.PassAIJudge = countLive(cands)
	r.Funnel.CaughtByAudio = r.Funnel.PassHardFilter - r.Funnel.PassAIJudge
	var sceneStarts []float64
	for _, s := range sc[1:] {
		sceneStarts = append(sceneStarts, s.Start)
	}
	cands = breaks.ApplySceneContext(cands, sceneStarts)

	budget := breaks.MaxBreaks(m.Info.Duration, p.MaxBreaksPerHour, p.MaxAdLoadPct, p.PodSeconds)
	r.Funnel.BreakBudget = budget
	r.Effective.Budget = budget
	// Placement-aware selection: a break no brand can take (all blocked, or no
	// fit) is rejected with its reason and the DP re-runs, so the next-best break
	// gets the slot. Bounded rounds keep the AI cost predictable.
	const maxRounds = 8
	for round := 0; round < maxRounds; round++ {
		picked := breaks.Select(cands, breaks.Limits{MaxBreaks: budget, MinGap: sp.MinGap, MinScore: p.MinScore})
		// Final speech check on each selected cut before anything is placed.
		failed := 0
		for _, i := range picked {
			v, err := d.Verify(ctx, ep, cands[i].T)
			if err != nil {
				return Result{}, err
			}
			if !v.Clear {
				cands[i].Rejected = RejectFinalCheck + ": " + v.Reason
				r.Funnel.FinalCheckRejects++
				failed++
			}
		}
		if failed > 0 {
			d.progress("select", "round %d: %d break(s) failed the final speech check, re-selecting", round+1, failed)
			continue
		}
		var sel []breaks.Candidate
		for _, i := range picked {
			sel = append(sel, cands[i])
		}
		placed, unplaced, err := d.placeAll(ctx, ep, sel, catalogue, tr, sc, p)
		if err != nil {
			return Result{}, err
		}
		r.Breaks = placed
		if len(unplaced) == 0 || round == maxRounds-1 {
			r.Unplaced = append(r.Unplaced, unplaced...)
			break
		}
		for _, u := range unplaced {
			r.Unplaced = append(r.Unplaced, u)
			for i := range cands {
				if cands[i].T == u.T {
					cands[i].Rejected = RejectNoBrand
				}
			}
		}
		d.progress("select", "round %d: %d break(s) had no eligible brand, re-selecting", round+1, len(unplaced))
	}
	r.Funnel.Placed = len(r.Breaks)
	r.Funnel.Suppressed = len(r.Unplaced)
	r.Funnel.Considered = r.Funnel.Placed + r.Funnel.Suppressed
	r.Candidates = cands
	r.normalize()

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

func nearestBoundary(sc []scenes.Scene, t float64) Boundary {
	best := Boundary{SceneIndex: -1}
	for _, s := range sc[min(1, len(sc)):] {
		if best.SceneIndex < 0 || abs(t-s.Start) < abs(best.Shift) {
			best = Boundary{SceneIndex: s.Index, SceneStart: s.Start, Shift: t - s.Start}
		}
	}
	switch {
	case best.SceneIndex < 0:
		best.Note = "no scene boundary in this episode"
	case abs(best.Shift) < 1:
		best.Note = fmt.Sprintf("at the start of scene %d", best.SceneIndex+1)
	case abs(best.Shift) <= 20:
		best.Note = fmt.Sprintf("scene %d starts at %s; nearest speech-safe point is %+.0fs from it because speech crosses the transition", best.SceneIndex+1, clock(best.SceneStart), best.Shift)
	default:
		best.Note = fmt.Sprintf("mid-scene: nearest scene start (scene %d, %s) is %+.0fs away; chosen for a clear pause, scored down x0.6", best.SceneIndex+1, clock(best.SceneStart), best.Shift)
	}
	return best
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func clock(sec float64) string { return fmt.Sprintf("%d:%02d", int(sec)/60, int(sec)%60) }

// placeAll runs placement and the independent safety check for each selected
// break, then decides in code. Shared by Run and Rematch.
func (d Deps) placeAll(ctx context.Context, ep Episode, sel []breaks.Candidate, catalogue []brands.Brand,
	tr speech.Transcript, sc []scenes.Scene, p Pacing) ([]Break, []Placement, error) {
	byID := map[string]brands.Brand{}
	for _, b := range catalogue {
		byID[b.ID] = b
	}
	negs := negativeUnion(catalogue)
	var placed []Break
	var unplaced []Placement
	for _, c := range sel {
		pl, err := d.Place(ctx, ep, c.T, catalogue, tr, p.PodSeconds)
		if err != nil {
			return nil, nil, err
		}
		checks, err := d.Safety(ctx, ep, c.T, negs)
		if err != nil {
			return nil, nil, err
		}
		flags := SafetyFlags(negs, checks, sc, c.T)
		// The decision is always recomputed in code from cached model answers.
		pl.Decision = brands.Decide(catalogue, pl.Verdicts, flags, p.PodSeconds)
		if pl.Decision.BrandID == "" {
			unplaced = append(unplaced, pl)
			continue
		}
		b := byID[pl.Decision.BrandID]
		br := Break{T: c.T, Score: c.Score, Rationale: c.Rationale, Placement: pl, BrandName: b.Name,
			Boundary: nearestBoundary(sc, c.T), Safety: checks, Flags: flags}
		for _, cr := range b.Creatives {
			if cr.ID == pl.Decision.CreativeID {
				cr := cr
				br.Creative = &cr
			}
		}
		placed = append(placed, br)
	}
	return placed, unplaced, nil
}

// Rematch re-places an analysed episode's selected breaks against a new
// catalogue (for example with a brand added at runtime). Where breaks fall does
// not change; only brand matching re-runs. Nothing about any brand is coded.
func (d Deps) Rematch(ctx context.Context, ep Episode, base Result, catalogue []brands.Brand, name string) (Result, error) {
	m, err := d.Media(ctx, ep)
	if err != nil {
		return Result{}, err
	}
	tr, err := d.Transcript(ctx, ep)
	if err != nil {
		return Result{}, err
	}
	sc, err := d.Scenes(ctx, ep, m, tr)
	if err != nil {
		return Result{}, err
	}
	// Only the breaks pacing kept are re-placed; breaks dropped earlier stay
	// dropped, so the min-gap and budget guarantees still hold.
	var sel []breaks.Candidate
	times := map[float64]bool{}
	for _, b := range base.Breaks {
		times[b.T] = true
	}
	for _, c := range base.Candidates {
		if times[c.T] {
			sel = append(sel, c)
		}
	}
	r := base
	r.ComputedAt = time.Now().UTC()
	var newlyUnplaced []Placement
	r.Breaks, newlyUnplaced, err = d.placeAll(ctx, ep, sel, catalogue, tr, sc, base.Pacing)
	if err != nil {
		return Result{}, err
	}
	r.Unplaced = append(append([]Placement(nil), base.Unplaced...), newlyUnplaced...)
	r.Funnel.Placed = len(r.Breaks)
	r.Funnel.Suppressed = len(r.Unplaced)
	r.Funnel.Considered = r.Funnel.Placed + r.Funnel.Suppressed
	r.Trial = name
	r.normalize()
	b, _ := json.MarshalIndent(r, "", "  ")
	return r, os.WriteFile(filepath.Join(ep.Dir, name+".json"), b, 0o644)
}

// normalize makes empty lists encode as [] rather than null in the debug JSON.
func (r *Result) normalize() {
	if r.Breaks == nil {
		r.Breaks = []Break{}
	}
	if r.Unplaced == nil {
		r.Unplaced = []Placement{}
	}
	if r.Scenes == nil {
		r.Scenes = []scenes.Scene{}
	}
	if r.Candidates == nil {
		r.Candidates = []breaks.Candidate{}
	}
}
