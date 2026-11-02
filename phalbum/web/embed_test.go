package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":           {Data: []byte("<html>app</html>")},
		"assets/app-1.js":      {Data: []byte("js")},
		"manifest.webmanifest": {Data: []byte("{}")},
	})
	cases := []struct {
		method, target string
		status         int
		body, cache    string
	}{
		{"GET", "/", 200, "app", "no-cache"},
		{"GET", "/photo/abc", 200, "app", "no-cache"},
		{"GET", "/index.html", 200, "app", "no-cache"},
		{"GET", "/assets/app-1.js", 200, "js", "immutable"},
		{"GET", "/manifest.webmanifest", 200, "{}", "no-cache"},
		{"GET", "/assets/gone.js", 404, "", ""},
		{"POST", "/", 405, "", ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.target, nil))
		if rec.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.target, rec.Code, c.status)
			continue
		}
		if !strings.Contains(rec.Body.String(), c.body) || !strings.Contains(rec.Header().Get("Cache-Control"), c.cache) {
			t.Errorf("%s %s: body %q cache %q", c.method, c.target, rec.Body, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestHandlerNotBuilt(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 503 {
		t.Errorf("status %d", rec.Code)
	}
}
