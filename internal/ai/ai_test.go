package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
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

type staticTokens struct{}

func (staticTokens) Token() (*oauth2.Token, error) { return &oauth2.Token{AccessToken: "tok"}, nil }

func TestVertexURLAndAuth(t *testing.T) {
	v := &Vertex{Project: "p1", Location: "global", Tokens: staticTokens{}}
	if got := v.url("gemini-x"); got != "https://aiplatform.googleapis.com/v1/projects/p1/locations/global/publishers/google/models/gemini-x:generateContent" {
		t.Fatal(got)
	}
	v.Location = "us-central1"
	if got := v.url("m"); !strings.HasPrefix(got, "https://us-central1-aiplatform.googleapis.com/v1/projects/p1/locations/us-central1/") {
		t.Fatal(got)
	}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{}"}]}}]}`))
	}))
	defer srv.Close()
	g := &Gemini{Model: "m", Vertex: v, Client: rewrite(srv.URL)}
	var out map[string]any
	if err := g.JSON(context.Background(), []Part{Text("x")}, map[string]any{"type": "object"}, &out); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tok" || g.Name() != "vertex:m" {
		t.Fatalf("auth=%q name=%q", auth, g.Name())
	}
}

// rewrite sends every request to target, so the Vertex URL can be exercised locally.
func rewrite(target string) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		u, _ := url.Parse(target)
		r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
