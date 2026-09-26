// Package ai is the single seam to the multimodal model. Callers describe what
// they want as parts plus a JSON schema; the provider returns decoded JSON.
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
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

// Gemini talks to the Generative Language REST API.
type Gemini struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
	calls   atomic.Int64
	tokens  atomic.Int64
}

func (g *Gemini) Name() string { return "gemini:" + g.Model }

// Usage reports calls made and total tokens billed so far.
func (g *Gemini) Usage() (calls, tokens int64) { return g.calls.Load(), g.tokens.Load() }

func (g *Gemini) JSON(ctx context.Context, parts []Part, schema map[string]any, out any) error {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)
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
