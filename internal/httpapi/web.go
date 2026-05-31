package httpapi

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// webDist holds the built SvelteKit frontend. The directory always contains at
// least a placeholder index.html (committed) so the package compiles before the
// frontend is built; `npm run build` overwrites it with the real assets.
//
//go:embed all:webdist
var webDist embed.FS

// spaHandler serves static assets from the embedded frontend and falls back to
// index.html for client-side routes (single-page app behavior).
func (s *Server) spaHandler() http.Handler {
	sub, err := fs.Sub(webDist, "webdist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if f, err := sub.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Unknown path: serve the SPA entrypoint.
		index, err := sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.Copy(w, index)
	})
}
