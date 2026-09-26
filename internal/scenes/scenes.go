// Package scenes groups shots into semantically coherent scenes. Cheap signals
// (colour histograms, speech gaps) propose boundaries; the model confirms them
// and describes each scene. The proposals alone are the fallback if the model fails.
package scenes

import (
	"image"
	_ "image/jpeg"
	"math"
	"os"
)

type Shot struct {
	Index int     `json:"index"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Frame string  `json:"frame"`
}

type Scene struct {
	Index       int      `json:"index"`
	Start       float64  `json:"start"`
	End         float64  `json:"end"`
	FirstShot   int      `json:"first_shot"`
	LastShot    int      `json:"last_shot"`
	Description string   `json:"description"`
	Activity    string   `json:"dominant_activity"`
	Contexts    []string `json:"contexts"`
	Mood        string   `json:"mood"`
	Source      string   `json:"source"` // "ai" or "heuristic"
}

// Shots turns cut times into shot spans covering [0, duration].
func Shots(cuts []float64, duration float64) []Shot {
	var out []Shot
	prev := 0.0
	for _, c := range append(append([]float64{}, cuts...), duration) {
		if c-prev < 0.04 {
			continue
		}
		out = append(out, Shot{Index: len(out), Start: prev, End: c})
		prev = c
	}
	return out
}

const bins = 4

// Histogram returns a normalised 4x4x4 RGB histogram of a JPEG.
func Histogram(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	h := make([]float64, bins*bins*bins)
	b := img.Bounds()
	n := 0.0
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			i := int(r>>14)*bins*bins + int(g>>14)*bins + int(bl>>14)
			h[i]++
			n++
		}
	}
	for i := range h {
		h[i] /= n
	}
	return h, nil
}

// Distance is 1 minus histogram intersection: 0 identical, 1 disjoint.
func Distance(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += math.Min(a[i], b[i])
	}
	return 1 - s
}

// Propose marks shot i (i>0) as a likely scene start when its colour changes a
// lot from the previous shot and nobody is speaking across the cut.
func Propose(hists [][]float64, speechAtCut []bool, threshold float64) []bool {
	out := make([]bool, len(hists))
	if len(out) > 0 {
		out[0] = true
	}
	for i := 1; i < len(hists); i++ {
		out[i] = Distance(hists[i-1], hists[i]) >= threshold && !speechAtCut[i]
	}
	return out
}

// Build turns per-shot "starts a scene" flags into scenes.
func Build(shots []Shot, starts []bool, source string) []Scene {
	var out []Scene
	for i, sh := range shots {
		if i == 0 || starts[i] {
			out = append(out, Scene{Index: len(out), Start: sh.Start, FirstShot: i, Source: source})
		}
		cur := &out[len(out)-1]
		cur.End, cur.LastShot = sh.End, i
	}
	return out
}
