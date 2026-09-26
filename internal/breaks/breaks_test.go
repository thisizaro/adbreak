package breaks

import (
	"math"
	"testing"

	"github.com/thisizaro/adbreak/internal/speech"
)

func TestFilterRejectsSpeechAndMargins(t *testing.T) {
	tr := speech.Transcript{Segments: []speech.Segment{{Start: 400, End: 410}}}
	cands := []Candidate{{T: 60}, {T: 405}, {T: 410.3}, {T: 411}, {T: 700}, {T: 1150}}
	rules := Rules{Duration: 1200, HeadMargin: 180, TailMargin: 120, SpeechMargin: 0.5}
	got := Filter(cands, tr, rules)
	want := []string{RejectHead, RejectSpeech, RejectSpeech, "", "", RejectTail}
	for i, w := range want {
		if got[i].Rejected != w {
			t.Errorf("t=%v rejected=%q, want %q", got[i].T, got[i].Rejected, w)
		}
	}
}

func TestSelectMaximisesScoreUnderGapAndCount(t *testing.T) {
	// Greedy would take 0.9 at 500 and then only fit one more; the DP finds that
	// 300 + 700 + 1100 (0.6+0.6+0.6) beats 500 + 1100 (0.9+0.6).
	cands := []Candidate{
		{T: 300, Score: 0.6}, {T: 500, Score: 0.9}, {T: 700, Score: 0.6}, {T: 1100, Score: 0.6},
	}
	idx := Select(cands, Limits{MaxBreaks: 3, MinGap: 350, MinScore: 0.3})
	if len(idx) != 3 || cands[idx[0]].T != 300 || cands[idx[1]].T != 700 || cands[idx[2]].T != 1100 {
		t.Fatalf("got %v", idx)
	}
}

func TestSelectRespectsCountThresholdAndRejected(t *testing.T) {
	cands := []Candidate{
		{T: 300, Score: 0.2}, {T: 700, Score: 0.8, Rejected: RejectSpeech}, {T: 1100, Score: 0.5}, {T: 1500, Score: 0.7},
	}
	idx := Select(cands, Limits{MaxBreaks: 1, MinGap: 100, MinScore: 0.3})
	if len(idx) != 1 || cands[idx[0]].T != 1500 {
		t.Fatalf("got %v", idx)
	}
	if got := Select(cands, Limits{MaxBreaks: 3, MinGap: 100, MinScore: 0.9}); len(got) != 0 {
		t.Fatalf("nothing clears the threshold, got %v", got)
	}
}

func TestMaxBreaks(t *testing.T) {
	cases := []struct {
		dur, perHour, load, pod float64
		want                    int
	}{
		{1227, 6, 15, 30, 2},  // 20.5 min at 6/h -> 2
		{2349, 6, 15, 30, 3},  // 39 min -> 3
		{1227, 6, 2.5, 30, 1}, // ad load caps it: 1 pod of 30s in 20.5 min is 2.4%
		{300, 6, 15, 30, 0},   // too short for a break at 6/h
	}
	for _, c := range cases {
		if got := MaxBreaks(c.dur, c.perHour, c.load, c.pod); got != c.want {
			t.Errorf("MaxBreaks(%v) = %d, want %d", c, got, c.want)
		}
	}
}

func TestApplySceneContext(t *testing.T) {
	cands := []Candidate{{T: 110, Score: 0.9}, {T: 500, Score: 0.9}, {T: 600, Score: 0.9, Rejected: RejectSpeech}}
	got := ApplySceneContext(cands, []float64{100, 480})
	if got[0].Score != 1 || got[1].Score < 0.99 || got[2].Score != 0.9 {
		t.Fatalf("near-boundary scores wrong: %+v", got)
	}
	got = ApplySceneContext([]Candidate{{T: 300, Score: 0.5}}, []float64{100})
	if got[0].Score != 0.3 || got[0].Signals[0] != "mid_scene" {
		t.Fatalf("mid-scene: %+v", got)
	}
}

func TestScale(t *testing.T) {
	s := Scale(1227, 180, 8, 120, 5, 240, 0.2) // 20.5 min
	if math.Abs(s.Head-98.16) > 0.01 || math.Abs(s.Tail-61.35) > 0.01 || math.Abs(s.MinGap-245.4) > 0.01 {
		t.Fatalf("short: %+v", s)
	}
	l := Scale(2700, 180, 8, 120, 5, 240, 0.2) // 45 min
	if l.Head != 180 || l.Tail != 120 || l.MinGap != 540 {
		t.Fatalf("long: %+v", l)
	}
}
