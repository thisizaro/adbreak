package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/speech"
)

// RejectFinalCheck marks a selected break that failed the final speech check.
const RejectFinalCheck = "final speech check"

// Verification is the last gate before a break is placed. The long-form
// transcript can silently drop dialogue and the batched audio check can miss
// it, so each selected cut is re-examined on its own by three independent
// signals: two model hearings decide, a short-window Whisper transcript is kept
// as evidence. Anything other than a clear "no speech" from both models rejects it.
type Verification struct {
	T       float64          `json:"t"`
	Whisper []speech.Segment `json:"whisper_window"`
	Bulk    Hearing          `json:"bulk_model"`
	Decide  Hearing          `json:"decide_model"`
	Clear   bool             `json:"clear"`
	Reason  string           `json:"reason,omitempty"`
}

type Hearing struct {
	SpeechAtCut string `json:"speech_at_cut"`
	Heard       string `json:"heard"`
}

const (
	verifyWindow = 6.0 // seconds either side for the Whisper re-transcription
	verifyClip   = 3.0 // seconds either side for the model question
	// A re-transcribed segment counts as speech at the cut if it covers the
	// cut within this margin and Whisper itself thinks it is speech.
	verifyMargin   = 0.4
	verifyNoSpeech = 0.5
)

var hearingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"speech_at_cut": map[string]any{"type": "string", "enum": []string{"yes", "no", "unsure"}},
		"heard":         map[string]any{"type": "string"},
	},
	"required": []string{"speech_at_cut", "heard"},
}

const hearingPrompt = `This 6 second clip is centred on a video cut at exactly 3.0 seconds. Answer strictly: is a human voice
speaking (dialogue, narration, or a sentence continuing) at or within 0.5 seconds of the 3.0 second mark? Singing
counts as a voice. Answer "no" only if you are sure nobody is speaking there; otherwise "yes" or "unsure".
Also say briefly what you hear across the whole clip.`

// WhisperCoversCut reports whether any speech segment (in episode time) covers t.
func WhisperCoversCut(segs []speech.Segment, t float64) (bool, string) {
	for _, s := range segs {
		if s.NoSpeech < verifyNoSpeech && s.Start <= t+verifyMargin && s.End >= t-verifyMargin && s.Text != "" {
			return true, fmt.Sprintf("Whisper hears %q from %.1fs to %.1fs", s.Text, s.Start, s.End)
		}
	}
	return false, ""
}

// judgeVerification decides from the two model hearings; kept separate so it is
// testable. The short-window Whisper transcript is recorded as evidence only:
// on 12 s windows it hallucinates repetitive text over music and its segments
// span the whole window, so as a gate it rejected almost every music interlude.
func judgeVerification(v *Verification) {
	v.Clear, v.Reason = true, ""
	for _, h := range []struct {
		name string
		h    Hearing
	}{{"bulk model", v.Bulk}, {"placement model", v.Decide}} {
		if h.h.SpeechAtCut != "no" {
			v.Clear, v.Reason = false, fmt.Sprintf("%s: %s (%s)", h.name, orUnsure(h.h.SpeechAtCut), h.h.Heard)
			return
		}
	}
}

func orUnsure(s string) string {
	if s == "" {
		return "no answer"
	}
	return s
}

func (d Deps) Verify(ctx context.Context, ep Episode, t float64) (Verification, error) {
	v, err := d.verify(ctx, ep, t)
	if err == nil {
		judgeVerification(&v) // re-decide from cached hearings so rule changes apply without new calls
	}
	return v, err
}

func (d Deps) verify(ctx context.Context, ep Episode, t float64) (Verification, error) {
	return cached(ep.Dir, fmt.Sprintf("verify_%.2f.json", t), func() (Verification, error) {
		work := filepath.Join(ep.Dir, "verify")
		if err := os.MkdirAll(work, 0o755); err != nil {
			return Verification{}, err
		}
		v := Verification{T: t}
		start := max(0, t-verifyWindow)
		win := filepath.Join(work, fmt.Sprintf("w%.2f.mp3", t))
		if err := media.AudioClip(ctx, ep.Video, start, 2*verifyWindow, win); err != nil {
			return v, err
		}
		tr, err := d.ASR.Transcribe(ctx, win)
		if err != nil {
			return v, err
		}
		for _, s := range tr.Segments {
			s.Start += start
			s.End += start
			v.Whisper = append(v.Whisper, s)
		}
		clip := filepath.Join(work, fmt.Sprintf("c%.2f.mp3", t))
		if err := media.AudioClip(ctx, ep.Video, t-verifyClip, 2*verifyClip, clip); err != nil {
			return v, err
		}
		b, err := os.ReadFile(clip)
		if err != nil {
			return v, err
		}
		parts := []ai.Part{ai.Text(hearingPrompt), ai.Blob("audio/mp3", b)}
		if err := d.AI.JSON(ctx, parts, hearingSchema, &v.Bulk); err != nil {
			return v, err
		}
		decide := d.Decide
		if decide == nil {
			decide = d.AI
		}
		if err := decide.JSON(ctx, parts, hearingSchema, &v.Decide); err != nil {
			return v, err
		}
		judgeVerification(&v)
		d.progress("verify", "t=%.1f clear=%v %s", t, v.Clear, v.Reason)
		return v, nil
	})
}
