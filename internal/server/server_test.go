package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRoutes(t *testing.T) {
	static := fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("js")},
	}
	h := New("test", static).Handler()

	cases := []struct {
		path, want string
		code       int
	}{
		{"/api/health", `"status":"ok"`, 200},
		{"/api/missing", `"error":"not found"`, 404},
		{"/", "spa", 200},
		{"/episodes/42", "spa", 200},
		{"/assets/app.js", "js", 200},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: got %d %q, want %d containing %q", c.path, rec.Code, rec.Body.String(), c.code, c.want)
		}
	}
}
