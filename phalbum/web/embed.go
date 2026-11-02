// Package web embeds the built web app (npm run build writes dist/) and
// serves it as a single-page app.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the files in dist/. Paths that aren't files get index.html,
// so client-side routes like /photo/{id} survive a reload. Hashed files under
// /assets/ are cached forever; everything else is revalidated.
func Handler() http.Handler {
	files, _ := fs.Sub(dist, "dist")
	return handler(files)
}

func handler(files fs.FS) http.Handler {
	index, _ := fs.ReadFile(files, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(files, name); err == nil && !info.IsDir() && name != "index.html" {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, files, name)
			return
		}
		if path.Ext(name) != "" && name != "index.html" {
			http.NotFound(w, r) // a missing asset, not a client-side route
			return
		}
		if index == nil {
			http.Error(w, "web app not built: run `npm run build` in phalbum/web", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
