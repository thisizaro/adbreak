// Package speech turns audio into timed speech spans. The Provider interface is
// the seam for swapping hosted Whisper for a self-hosted one.
package speech

import (
	"context"
	"sort"
)

type Word struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type Transcript struct {
	Segments []Segment `json:"segments"`
	Words    []Word    `json:"words"`
}

type Provider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (Transcript, error)
}

// Part is one chunk's transcript in chunk-local time.
type Part struct {
	Offset float64
	Length float64
	T      Transcript
}

// Merge stitches chunk transcripts into episode time. In each overlap the seam
// is its midpoint: words are taken from the earlier chunk before it and from the
// later chunk after it. Segments are assigned by start time the same way, so a
// segment that straddles the seam survives whole.
func Merge(parts []Part) Transcript {
	var out Transcript
	for i, p := range parts {
		lo, hi := -1e18, 1e18
		if i > 0 {
			prev := parts[i-1]
			lo = (p.Offset + prev.Offset + prev.Length) / 2
		}
		if i < len(parts)-1 {
			next := parts[i+1]
			hi = (next.Offset + p.Offset + p.Length) / 2
		}
		for _, w := range p.T.Words {
			w.Start += p.Offset
			w.End += p.Offset
			if w.Start >= lo && w.Start < hi {
				out.Words = append(out.Words, w)
			}
		}
		for _, s := range p.T.Segments {
			s.Start += p.Offset
			s.End += p.Offset
			if s.Start >= lo && s.Start < hi {
				out.Segments = append(out.Segments, s)
			}
		}
	}
	sort.Slice(out.Words, func(a, b int) bool { return out.Words[a].Start < out.Words[b].Start })
	sort.Slice(out.Segments, func(a, b int) bool { return out.Segments[a].Start < out.Segments[b].Start })
	return out
}

// SpeechNear reports whether t lies inside any segment or word span widened by margin seconds.
func (tr Transcript) SpeechNear(t, margin float64) bool {
	for _, s := range tr.Segments {
		if t >= s.Start-margin && t <= s.End+margin {
			return true
		}
	}
	for _, w := range tr.Words {
		if t >= w.Start-margin && t <= w.End+margin {
			return true
		}
	}
	return false
}

// TextBetween returns the segment text overlapping [from, to].
func (tr Transcript) TextBetween(from, to float64) string {
	var s string
	for _, seg := range tr.Segments {
		if seg.End >= from && seg.Start <= to {
			s += seg.Text + " "
		}
	}
	return s
}
