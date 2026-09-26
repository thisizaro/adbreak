package speech

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGroqTranscribeParsesAndRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("model") != "whisper-large-v3" || r.FormValue("language") != "bn" ||
			len(r.MultipartForm.Value["timestamp_granularities[]"]) != 2 {
			t.Errorf("bad form: %v", r.MultipartForm.Value)
		}
		w.Write([]byte(`{"segments":[{"start":1.0,"end":2.5,"text":" hello "}],"words":[{"word":"hello","start":1.1,"end":1.6}]}`))
	}))
	defer srv.Close()

	audio := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(audio, []byte("fake"), 0o644)
	g := &Groq{BaseURL: srv.URL, APIKey: "k", Model: "whisper-large-v3", Language: "bn"}
	tr, err := g.Transcribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(tr.Segments) != 1 || tr.Segments[0].Text != "hello" || tr.Words[0].End != 1.6 {
		t.Fatalf("calls=%d tr=%+v", calls, tr)
	}
}
