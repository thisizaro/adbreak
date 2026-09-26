package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// maxChunk keeps every media response under Cloud Run's 32 MiB HTTP/1 response
// cap. Browsers treat a shorter 206 as normal and request the next range.
const maxChunk = 16 << 20

// serveChunked serves path honouring Range, but never more than maxChunk bytes.
func serveChunked(w http.ResponseWriter, r *http.Request, path string) {
	st, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	start, end := int64(0), st.Size()-1
	if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") && !strings.Contains(h, ",") {
		a, b, _ := strings.Cut(strings.TrimPrefix(h, "bytes="), "-")
		switch {
		case a == "" && b != "": // suffix range: last N bytes
			n, _ := strconv.ParseInt(b, 10, 64)
			start = max(0, st.Size()-n)
		default:
			start, _ = strconv.ParseInt(a, 10, 64)
			if b != "" {
				if e, err := strconv.ParseInt(b, 10, 64); err == nil && e < end {
					end = e
				}
			}
		}
	}
	if start > end || start >= st.Size() {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", st.Size()))
		http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end-start+1 > maxChunk {
		end = start + maxChunk - 1
	}
	r2 := r.Clone(r.Context())
	r2.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	http.ServeFile(w, r2, path)
}
