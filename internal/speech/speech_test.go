package speech

import "testing"

func TestMergeChunksDropsOverlapDuplicates(t *testing.T) {
	// Chunk 0 covers 0..10, chunk 1 covers 8..18 (2s overlap, cut at 9.0).
	c0 := Transcript{
		Segments: []Segment{{Start: 0, End: 4, Text: "a"}, {Start: 7.5, End: 9.8, Text: "b"}},
		Words:    []Word{{Start: 1, End: 1.5, Text: "one"}, {Start: 8.2, End: 8.6, Text: "two"}, {Start: 9.3, End: 9.7, Text: "three"}},
	}
	c1 := Transcript{ // local times, offset 8
		Segments: []Segment{{Start: 0, End: 1.8, Text: "b"}, {Start: 3, End: 6, Text: "c"}},
		Words:    []Word{{Start: 0.2, End: 0.6, Text: "two"}, {Start: 1.3, End: 1.7, Text: "three"}, {Start: 3.5, End: 4, Text: "four"}},
	}
	got := Merge([]Part{{Offset: 0, Length: 10, T: c0}, {Offset: 8, Length: 10, T: c1}})

	want := []string{"one", "two", "three", "four"}
	if len(got.Words) != len(want) {
		t.Fatalf("words = %+v", got.Words)
	}
	for i, w := range want {
		if got.Words[i].Text != w {
			t.Fatalf("word %d = %q, want %q (%+v)", i, got.Words[i].Text, w, got.Words)
		}
	}
	if got.Words[3].Start != 11.5 {
		t.Fatalf("offset not applied: %+v", got.Words[3])
	}
	// Segments straddling the seam are kept whole from whichever chunk they
	// start in; spans are what the speech guard needs, so none may be lost.
	if len(got.Segments) != 3 || got.Segments[1].End < 9.8 {
		t.Fatalf("segments = %+v", got.Segments)
	}
}

func TestSpeechAt(t *testing.T) {
	tr := Transcript{Segments: []Segment{{Start: 10, End: 12}, {Start: 20, End: 25}}}
	cases := []struct {
		t      float64
		margin float64
		want   bool
	}{
		{11, 0, true}, {12.3, 0.5, true}, {12.6, 0.5, false}, {19.6, 0.5, true}, {16, 0.5, false},
	}
	for _, c := range cases {
		if got := tr.SpeechNear(c.t, c.margin); got != c.want {
			t.Errorf("SpeechNear(%v, %v) = %v, want %v", c.t, c.margin, got, c.want)
		}
	}
}
