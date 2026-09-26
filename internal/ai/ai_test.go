package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiJSON(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/models/m:generateContent") || r.Header.Get("x-goog-api-key") != "k" {
			t.Errorf("bad request %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
		if len(parts) != 2 || parts[1].(map[string]any)["inline_data"] == nil {
			t.Errorf("parts = %v", parts)
		}
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"ok\":true}"}]}}],"usageMetadata":{"totalTokenCount":42}}`))
	}))
	defer srv.Close()
	g := &Gemini{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	var out struct{ OK bool }
	if err := g.JSON(context.Background(), []Part{Text("hi"), Blob("image/jpeg", []byte{1})}, map[string]any{"type": "object"}, &out); err != nil {
		t.Fatal(err)
	}
	if c, tok := g.Usage(); !out.OK || c != 2 || tok != 42 {
		t.Fatalf("out=%v calls=%d tokens=%d", out, c, tok)
	}
}
