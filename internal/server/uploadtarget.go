package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/oauth2"
)

// UploadTarget hands the browser somewhere to PUT video bytes.
type UploadTarget interface {
	Begin(ctx context.Context, id, origin string, size int64) (string, error)
}

// LocalTarget receives uploads on this server (dev only; Cloud Run caps bodies at 32 MiB).
type LocalTarget struct{}

func (LocalTarget) Begin(_ context.Context, id, _ string, _ int64) (string, error) {
	return "/api/uploads/" + id, nil
}

// GCSTarget opens a resumable upload session so the browser sends bytes
// straight to Cloud Storage. The session URI is the credential; the object
// lands at Prefix+id+".mp4", which the service sees through its bucket mount.
type GCSTarget struct {
	Bucket string
	Prefix string
	Tokens oauth2.TokenSource
	Client *http.Client
}

func (g GCSTarget) Begin(ctx context.Context, id, origin string, size int64) (string, error) {
	name := g.Prefix + id + ".mp4"
	u := fmt.Sprintf("https://storage.googleapis.com/upload/storage/v1/b/%s/o?uploadType=resumable&name=%s",
		url.PathEscape(g.Bucket), url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return "", err
	}
	tok, err := g.Tokens.Token()
	if err != nil {
		return "", fmt.Errorf("gcs token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("X-Upload-Content-Type", "video/mp4")
	if size > 0 {
		// GCS then refuses any upload whose length differs from what was declared.
		req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
	}
	if origin != "" {
		req.Header.Set("Origin", origin) // lets the browser PUT cross-origin to the session
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gcs session: %w", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusOK || loc == "" {
		return "", fmt.Errorf("gcs session: HTTP %d", resp.StatusCode)
	}
	return loc, nil
}
