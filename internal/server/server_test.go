package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/library"
	"github.com/thisizaro/adbreak/internal/pipeline"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	data, videos := t.TempDir(), t.TempDir()
	res := pipeline.Result{Episode: "ep1", Version: "t", Breaks: []pipeline.Break{
		{T: 917, BrandName: "Brand A", Creative: &brands.Creative{ID: "a_30s_bn", Seconds: 30}},
	}}
	os.MkdirAll(filepath.Join(data, "ep1"), 0o755)
	b, _ := json.Marshal(res)
	os.WriteFile(filepath.Join(data, "ep1", "debug.json"), b, 0o644)
	os.WriteFile(filepath.Join(videos, "ep1.mp4"), []byte("0123456789"), 0o644)
	lib := &library.Library{DataDir: data, VideoDir: videos, Catalogue: func() []brands.Brand { return nil }}
	static := fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}, "assets/app.js": {Data: []byte("js")}}
	return New("test", static, lib, pipeline.Pacing{}).Handler()
}

func TestRoutes(t *testing.T) {
	h := newTestServer(t)
	cases := []struct {
		path, want string
		code       int
	}{
		{"/api/health", `"status":"ok"`, 200},
		{"/api/missing", `"error":"not found"`, 404},
		{"/api/episodes", `"break_times":[917]`, 200},
		{"/api/episodes/ep1", `"pipeline_version":"t"`, 200},
		{"/api/episodes/nope", `not found`, 404},
		{"/api/episodes/ep1/vmap.xml", `timeOffset="00:15:17.000"`, 200},
		{"/media/episodes/ep1.mp4", "0123456789", 206},
		{"/media/episodes/..%2fx.mp4", "not found", 404},
		{"/media/slates/unknown.mp4", "not found", 404},
		{"/", "spa", 200},
		{"/episodes/ep1", "spa", 200},
		{"/assets/app.js", "js", 200},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: got %d %.200q, want %d containing %q", c.path, rec.Code, rec.Body.String(), c.code, c.want)
		}
	}
}

func TestVideoSupportsRangeRequests(t *testing.T) {
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/media/episodes/ep1.mp4", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
