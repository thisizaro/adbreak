package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServeChunkedClampsLargeRanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.bin")
	size := int64(maxChunk*2 + 100)
	f, _ := os.Create(p)
	f.Truncate(size)
	f.Close()
	cases := []struct {
		rng       string
		wantCode  int
		wantRange string
		wantLen   int64
	}{
		{"", 206, "bytes 0-16777215/33554532", maxChunk},
		{"bytes=0-", 206, "bytes 0-16777215/33554532", maxChunk},
		{"bytes=100-199", 206, "bytes 100-199/33554532", 100},
		{"bytes=33554500-", 206, "bytes 33554500-33554531/33554532", 32},
		{"bytes=-10", 206, "bytes 33554522-33554531/33554532", 10},
		{"bytes=99999999-", 416, "", 0},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if c.rng != "" {
			req.Header.Set("Range", c.rng)
		}
		rec := httptest.NewRecorder()
		serveChunked(rec, req, p)
		if rec.Code != c.wantCode || rec.Header().Get("Content-Range") != c.wantRange && c.wantCode == 206 || (c.wantCode == 206 && int64(rec.Body.Len()) != c.wantLen) {
			t.Errorf("%q: code=%d range=%q len=%d", c.rng, rec.Code, rec.Header().Get("Content-Range"), rec.Body.Len())
		}
	}
}
