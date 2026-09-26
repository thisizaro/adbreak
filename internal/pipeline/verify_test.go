package pipeline

import (
	"testing"

	"github.com/thisizaro/adbreak/internal/speech"
)

func TestJudgeVerificationFailsClosed(t *testing.T) {
	no := Hearing{SpeechAtCut: "no", Heard: "music"}
	cases := []struct {
		name  string
		v     Verification
		clear bool
	}{
		{"all clear", Verification{T: 100, Bulk: no, Decide: no}, true},
		{"whisper text is evidence only", Verification{T: 100, Whisper: []speech.Segment{{Start: 94, End: 106, Text: "khon khon khon", NoSpeech: 0.01}}, Bulk: no, Decide: no}, true},
		{"both models hear speech", Verification{T: 100, Bulk: Hearing{SpeechAtCut: "yes"}, Decide: Hearing{SpeechAtCut: "yes"}}, false},
		{"bulk model unsure", Verification{T: 100, Bulk: Hearing{SpeechAtCut: "unsure"}, Decide: no}, false},
		{"decide model hears speech", Verification{T: 100, Bulk: no, Decide: Hearing{SpeechAtCut: "yes", Heard: "a man speaking"}}, false},
		{"missing answer", Verification{T: 100, Bulk: no}, false},
	}
	for _, c := range cases {
		judgeVerification(&c.v)
		if c.v.Clear != c.clear {
			t.Errorf("%s: clear=%v reason=%q", c.name, c.v.Clear, c.v.Reason)
		}
	}
}
