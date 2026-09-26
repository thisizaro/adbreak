// Package ai is the single seam to the multimodal model. Callers describe what
// they want as parts plus a JSON schema; the provider returns decoded JSON.
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Part struct {
	Text     string
	MIMEType string
	Data     []byte
}

func Text(s string) Part                 { return Part{Text: s} }
func Blob(mime string, data []byte) Part { return Part{MIMEType: mime, Data: data} }

type Provider interface {
	Name() string
	// JSON sends parts with a response schema and decodes the reply into out.
	JSON(ctx context.Context, parts []Part, schema map[string]any, out any) error
}

// Gemini talks to either the Generative Language API (AI Studio key) or
// Vertex AI / Agent Platform (OAuth via Application Default Credentials).
// Request and response bodies are the same; only the URL and auth differ.
type Gemini struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client

	// Vertex is set when calls go through aiplatform.googleapis.com.
	Vertex *Vertex
	// TokenBudget stops further calls once this many tokens were billed (0 = no cap).
	TokenBudget int64

	calls  atomic.Int64
	tokens atomic.Int64
}

type Vertex struct {
	Project  string
	Location string // "global" or a region such as "us-central1"
	Tokens   oauth2.TokenSource
}

func (v *Vertex) url(model string) string {
	host := "aiplatform.googleapis.com"
	if v.Location != "global" {
		host = v.Location + "-aiplatform.googleapis.com"
	}
	return fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent", host, v.Project, v.Location, model)
}

// NewVertex builds a Vertex backend from Application Default Credentials.
func NewVertex(ctx context.Context, project, location string) (*Vertex, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("vertex credentials: %w", err)
	}
	if project == "" {
		project = creds.ProjectID
	}
	if project == "" {
		return nil, fmt.Errorf("vertex: no project (set GCP_PROJECT)")
	}
	return &Vertex{Project: project, Location: location, Tokens: creds.TokenSource}, nil
}

func (g *Gemini) Name() string {
	if g.Vertex != nil {
		return "vertex:" + g.Model
	}
	return "gemini:" + g.Model
}

// Usage reports calls made and total tokens billed so far.
func (g *Gemini) Usage() (calls, tokens int64) { return g.calls.Load(), g.tokens.Load() }

// ErrBudget is returned once a provider has used its token budget.
var ErrBudget = errors.New("ai: token budget exhausted for this job")

func (g *Gemini) JSON(ctx context.Context, parts []Part, schema map[string]any, out any) error {
	if g.TokenBudget > 0 && g.tokens.Load() >= g.TokenBudget {
		return ErrBudget
	}
	var reqParts []map[string]any
	for _, p := range parts {
		if p.Data != nil {
			reqParts = append(reqParts, map[string]any{"inline_data": map[string]any{
				"mime_type": p.MIMEType, "data": base64.StdEncoding.EncodeToString(p.Data)}})
		} else {
			reqParts = append(reqParts, map[string]any{"text": p.Text})
		}
	}
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": reqParts}},
		"generationConfig": map[string]any{
			"temperature":      0,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
			"mediaResolution":  "MEDIA_RESOLUTION_LOW",
		},
	})
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		text, retry, err := g.once(ctx, body)
		if err == nil {
			if err := json.Unmarshal([]byte(text), out); err != nil {
				return fmt.Errorf("%s: decode reply: %w: %.300s", g.Name(), err, text)
			}
			return nil
		}
		lastErr = err
		if !retry {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(2<<attempt) * time.Second):
		}
	}
	return lastErr
}

func (g *Gemini) once(ctx context.Context, body []byte) (string, bool, error) {
	url := fmt.Sprintf("%s/models/%s:generateContent", strings.TrimRight(g.BaseURL, "/"), g.Model)
	if g.Vertex != nil {
		url = g.Vertex.url(g.Model)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.Vertex != nil {
		tok, err := g.Vertex.Tokens.Token()
		if err != nil {
			return "", false, fmt.Errorf("%s: token: %w", g.Name(), err)
		}
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	} else {
		req.Header.Set("x-goog-api-key", g.APIKey)
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	g.calls.Add(1)
	resp, err := client.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("%s: %w", g.Name(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", retry, fmt.Errorf("%s: HTTP %d: %.1500s", g.Name(), resp.StatusCode, raw)
	}
	var v struct {
		Candidates []struct {
			Content struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false, fmt.Errorf("%s: decode: %w", g.Name(), err)
	}
	g.tokens.Add(v.UsageMetadata.TotalTokenCount)
	if len(v.Candidates) == 0 || len(v.Candidates[0].Content.Parts) == 0 {
		return "", true, fmt.Errorf("%s: empty reply: %.300s", g.Name(), raw)
	}
	var sb strings.Builder
	for _, p := range v.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	return sb.String(), false, nil
}

// Settings select and configure the model backend.
type Settings struct {
	Backend     string // "vertex" or "studio"
	Project     string
	Location    string
	APIKey      string
	BaseURL     string
	TokenBudget int64
}

// New returns a provider for model. For Vertex, pass a shared *Vertex so the
// credential lookup happens once.
func New(s Settings, v *Vertex, model string) *Gemini {
	g := &Gemini{BaseURL: s.BaseURL, APIKey: s.APIKey, Model: model, TokenBudget: s.TokenBudget}
	if s.Backend == "vertex" {
		g.Vertex = v
	}
	return g
}
