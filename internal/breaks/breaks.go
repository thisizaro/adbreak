// Package breaks turns candidate cut points into the selected ad breaks.
// Hard rules live here as code, so no model output can override them.
package breaks

import (
	"math"
	"sort"

	"github.com/thisizaro/adbreak/internal/speech"
)

const (
	RejectHead   = "within head margin"
	RejectTail   = "within tail margin"
	RejectSpeech = "inside or near speech"
)

type Candidate struct {
	T         float64  `json:"t"`
	Score     float64  `json:"score"`
	Signals   []string `json:"signals,omitempty"`
	Rejected  string   `json:"rejected,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
}

type Rules struct {
	Duration     float64
	HeadMargin   float64
	TailMargin   float64
	SpeechMargin float64
}

// Filter applies the hard filters and records the first reason a candidate fails.
func Filter(cands []Candidate, tr speech.Transcript, r Rules) []Candidate {
	out := make([]Candidate, len(cands))
	for i, c := range cands {
		switch {
		case c.T < r.HeadMargin:
			c.Rejected = RejectHead
		case c.T > r.Duration-r.TailMargin:
			c.Rejected = RejectTail
		case tr.SpeechNear(c.T, r.SpeechMargin):
			c.Rejected = RejectSpeech
		}
		out[i] = c
	}
	return out
}

type Limits struct {
	MaxBreaks int
	MinGap    float64
	MinScore  float64
}

// MaxBreaks is the break budget for an episode: the per-hour rate applied to
// its length, further capped so pods of podSec never exceed maxLoadPct of runtime.
func MaxBreaks(duration, perHour, maxLoadPct, podSec float64) int {
	byRate := int(math.Floor(duration / 3600 * perHour))
	byLoad := int(math.Floor(maxLoadPct / 100 * duration / podSec))
	return min(byRate, byLoad)
}

// Select picks the subset of eligible candidates with the highest total score
// such that consecutive picks are at least MinGap apart and at most MaxBreaks
// are chosen. Candidates below MinScore are never picked, so an episode with no
// good break gets none. DP over (candidate, picks so far); O(n^2 * k).
func Select(cands []Candidate, l Limits) []int {
	var elig []int
	for i, c := range cands {
		if c.Rejected == "" && c.Score >= l.MinScore {
			elig = append(elig, i)
		}
	}
	sort.Slice(elig, func(a, b int) bool { return cands[elig[a]].T < cands[elig[b]].T })
	n, k := len(elig), l.MaxBreaks
	if n == 0 || k <= 0 {
		return nil
	}
	// best[i][j]: max total using j picks with elig[i] as the last pick.
	best := make([][]float64, n)
	prev := make([][]int, n)
	for i := range best {
		best[i] = make([]float64, k+1)
		prev[i] = make([]int, k+1)
		for j := range best[i] {
			best[i][j] = math.Inf(-1)
			prev[i][j] = -1
		}
		best[i][1] = cands[elig[i]].Score
	}
	for i := 0; i < n; i++ {
		for p := 0; p < i; p++ {
			if cands[elig[i]].T-cands[elig[p]].T < l.MinGap {
				continue
			}
			for j := 2; j <= k; j++ {
				if v := best[p][j-1] + cands[elig[i]].Score; v > best[i][j] {
					best[i][j], prev[i][j] = v, p
				}
			}
		}
	}
	bi, bj, bv := -1, 0, math.Inf(-1)
	for i := 0; i < n; i++ {
		for j := 1; j <= k; j++ {
			if best[i][j] > bv {
				bi, bj, bv = i, j, best[i][j]
			}
		}
	}
	var picked []int
	for i, j := bi, bj; i >= 0 && j >= 1; i, j = prev[i][j], j-1 {
		picked = append(picked, elig[i])
	}
	sort.Slice(picked, func(a, b int) bool { return cands[picked[a]].T < cands[picked[b]].T })
	return picked
}
