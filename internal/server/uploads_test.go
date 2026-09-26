package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/jobs"
	"github.com/thisizaro/adbreak/internal/library"
	"github.com/thisizaro/adbreak/internal/pipeline"
)

func TestLocalUploadFinalizeAndJob(t *testing.T) {
	data, videos := t.TempDir(), t.TempDir()
	clip := filepath.Join(t.TempDir(), "c.mp4")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=160x90:d=2",
		"-f", "lavfi", "-i", "sine=d=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", clip).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, b)
	}
	ran := make(chan string, 1)
	runner := jobs.NewRunner(jobs.NewBus(), func(_ context.Context, j jobs.Job, _ func(string, string)) (string, error) {
		ran <- j.Episode
		return "", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx, 2)
	lib := &library.Library{DataDir: data, VideoDir: videos, Catalogue: func() []brands.Brand { return nil }}
	h := New("t", fstest.MapFS{"index.html": {Data: []byte("x")}}, lib, pipeline.Pacing{}).WithUploads(&Uploads{
		Target: LocalTarget{}, VideoDir: videos, MaxBytes: 10 << 20, MaxDuration: 60, Runner: runner}).Handler()

	do := func(method, path string, body []byte) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewReader(body)))
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}
	code, up := do("POST", "/api/uploads", nil)
	id, _ := up["id"].(string)
	if code != 201 || !strings.HasPrefix(up["upload_url"].(string), "/api/uploads/up_") {
		t.Fatalf("create: %d %v", code, up)
	}
	if code, _ := do("POST", "/api/uploads/"+id+"/finalize", nil); code != 404 {
		t.Fatalf("finalize before upload should 404, got %d", code)
	}
	b, _ := os.ReadFile(clip)
	if code, m := do("PUT", up["upload_url"].(string), b); code != 200 {
		t.Fatalf("put: %d %v", code, m)
	}
	if code, m := do("POST", "/api/uploads/"+id+"/finalize", nil); code != 200 || m["duration"].(float64) < 1.5 {
		t.Fatalf("finalize: %d %v", code, m)
	}
	if code, m := do("POST", "/api/jobs", []byte(`{"episode":"`+id+`"}`)); code != 202 {
		t.Fatalf("job: %d %v", code, m)
	}
	if got := <-ran; got != id {
		t.Fatalf("ran %q", got)
	}
	// Oversized uploads are refused before any bytes move.
	if code, _ := do("POST", "/api/uploads", []byte(`{"size": 99999999999}`)); code != 413 {
		t.Fatalf("oversize: %d", code)
	}
	// A non-video is rejected at finalize and removed.
	_, up2 := do("POST", "/api/uploads", nil)
	id2 := up2["id"].(string)
	do("PUT", up2["upload_url"].(string), []byte("not a video"))
	if code, _ := do("POST", "/api/uploads/"+id2+"/finalize", nil); code != 422 {
		t.Fatalf("junk finalize: %d", code)
	}
	if _, err := os.Stat(filepath.Join(videos, id2+".mp4")); !os.IsNotExist(err) {
		t.Fatal("rejected upload not removed")
	}
	_ = http.StatusOK
}

func TestTryBrandRejectsCollidingIDs(t *testing.T) {
	data, videos := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(data, "ep1"), 0o755)
	os.WriteFile(filepath.Join(data, "ep1", "debug.json"), []byte(`{"episode":"ep1"}`), 0o644)
	cat := []brands.Brand{{ID: "brand_a", Creatives: []brands.Creative{{ID: "a_30s_bn", Seconds: 30}}}}
	lib := &library.Library{DataDir: data, VideoDir: videos, Catalogue: func() []brands.Brand { return cat }}
	runner := jobs.NewRunner(jobs.NewBus(), func(context.Context, jobs.Job, func(string, string)) (string, error) { return "", nil })
	h := New("t", fstest.MapFS{"index.html": {Data: []byte("x")}}, lib, pipeline.Pacing{}).WithUploads(&Uploads{Target: LocalTarget{}, Runner: runner}).Handler()
	cases := map[string]string{
		`{"brand_id":"brand_a","display_name":"A","target_contexts":["x"],"creatives":[{"id":"z_20s","duration_sec":20}]}`:    "already in the catalogue",
		`{"brand_id":"brand_z","display_name":"Z","target_contexts":["x"],"creatives":[{"id":"a_30s_bn","duration_sec":30}]}`: "already belongs to brand_a",
	}
	for body, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/episodes/ep1/try-brand", strings.NewReader(body)))
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("got %d %s, want 400 containing %q", rec.Code, rec.Body.String(), want)
		}
	}
}

func TestDeleteUpload(t *testing.T) {
	data, videos := t.TempDir(), t.TempDir()
	for _, id := range []string{"up_123", "mohanagar"} {
		os.MkdirAll(filepath.Join(data, id), 0o755)
		os.WriteFile(filepath.Join(data, id, "debug.json"), []byte(`{}`), 0o644)
		os.WriteFile(filepath.Join(videos, id+".mp4"), []byte("v"), 0o644)
	}
	lib := &library.Library{DataDir: data, VideoDir: videos, Catalogue: func() []brands.Brand { return nil }}
	runner := jobs.NewRunner(jobs.NewBus(), func(context.Context, jobs.Job, func(string, string)) (string, error) { return "", nil })
	h := New("t", fstest.MapFS{"index.html": {Data: []byte("x")}}, lib, pipeline.Pacing{}).WithUploads(&Uploads{Target: LocalTarget{}, Runner: runner}).Handler()
	del := func(id string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/episodes/"+id, nil))
		return rec.Code
	}
	if c := del("mohanagar"); c != 403 {
		t.Fatalf("sample delete: %d", c)
	}
	if c := del("up_123"); c != 200 {
		t.Fatalf("upload delete: %d", c)
	}
	if _, err := os.Stat(filepath.Join(data, "up_123")); !os.IsNotExist(err) {
		t.Fatal("cache dir not removed")
	}
	if _, err := os.Stat(filepath.Join(videos, "up_123.mp4")); !os.IsNotExist(err) {
		t.Fatal("video not removed")
	}
	if _, err := os.Stat(filepath.Join(videos, "mohanagar.mp4")); err != nil {
		t.Fatal("sample touched")
	}
	if c := del("up_123"); c != 404 {
		t.Fatalf("second delete: %d", c)
	}
}
