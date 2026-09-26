package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Groq calls an OpenAI-compatible /audio/transcriptions endpoint (Groq hosts
// open-weights Whisper) with verbose_json and word + segment timestamps.
type Groq struct {
	BaseURL  string
	APIKey   string
	Model    string
	Language string
	Client   *http.Client
}

func (g *Groq) Name() string { return "groq:" + g.Model }

func (g *Groq) Transcribe(ctx context.Context, audioPath string) (Transcript, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		tr, retry, err := g.once(ctx, audioPath)
		if err == nil {
			return tr, nil
		}
		lastErr = err
		if !retry {
			break
		}
		select {
		case <-ctx.Done():
			return Transcript{}, ctx.Err()
		case <-time.After(time.Duration(2<<attempt) * time.Second):
		}
	}
	return Transcript{}, lastErr
}

func (g *Groq) once(ctx context.Context, audioPath string) (Transcript, bool, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return Transcript{}, false, err
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return Transcript{}, false, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return Transcript{}, false, err
	}
	fields := [][2]string{
		{"model", g.Model},
		{"response_format", "verbose_json"},
		{"timestamp_granularities[]", "word"},
		{"timestamp_granularities[]", "segment"},
		{"temperature", "0"},
	}
	if g.Language != "" {
		fields = append(fields, [2]string{"language", g.Language})
	}
	for _, kv := range fields {
		_ = mw.WriteField(kv[0], kv[1])
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.BaseURL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return Transcript{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+g.APIKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Transcript{}, true, fmt.Errorf("groq transcribe: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return Transcript{}, retry, fmt.Errorf("groq transcribe: HTTP %d: %.300s", resp.StatusCode, raw)
	}
	var v struct {
		Segments []struct {
			Start, End float64
			Text       string
			NoSpeech   float64 `json:"no_speech_prob"`
		} `json:"segments"`
		Words []struct {
			Word       string
			Start, End float64
		} `json:"words"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Transcript{}, false, fmt.Errorf("groq transcribe: decode: %w", err)
	}
	var tr Transcript
	for _, s := range v.Segments {
		tr.Segments = append(tr.Segments, Segment{Start: s.Start, End: s.End, Text: strings.TrimSpace(s.Text), NoSpeech: s.NoSpeech})
	}
	for _, w := range v.Words {
		tr.Words = append(tr.Words, Word{Start: w.Start, End: w.End, Text: strings.TrimSpace(w.Word)})
	}
	return tr, false, nil
}
