package scenes

import "testing"

func TestShots(t *testing.T) {
	s := Shots([]float64{2, 2.01, 5}, 8)
	if len(s) != 3 || s[1].Start != 2 || s[1].End != 5 || s[2].End != 8 {
		t.Fatalf("%+v", s)
	}
}

func TestDistance(t *testing.T) {
	a := []float64{0.5, 0.5, 0, 0}
	if d := Distance(a, a); d != 0 {
		t.Fatal(d)
	}
	if d := Distance(a, []float64{0, 0, 0.5, 0.5}); d != 1 {
		t.Fatal(d)
	}
}

func TestProposeAndBuild(t *testing.T) {
	red, blue := []float64{1, 0}, []float64{0, 1}
	hists := [][]float64{red, red, blue, blue, red}
	speech := []bool{false, false, false, false, true} // speech across the last cut
	starts := Propose(hists, speech, 0.5)
	want := []bool{true, false, true, false, false}
	for i := range want {
		if starts[i] != want[i] {
			t.Fatalf("starts = %v", starts)
		}
	}
	shots := Shots([]float64{1, 2, 3, 4}, 5)
	sc := Build(shots, starts, "heuristic")
	if len(sc) != 2 || sc[0].End != 2 || sc[1].FirstShot != 2 || sc[1].LastShot != 4 || sc[1].End != 5 {
		t.Fatalf("%+v", sc)
	}
}
