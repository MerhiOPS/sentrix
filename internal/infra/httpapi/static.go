package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spa serves the built dashboard. Unknown paths fall back to index.html so
// client-side routes (/hosts/…) survive a reload; /api/ never reaches here.
func spa(web fs.FS) http.Handler {
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(web, p); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			files.ServeHTTP(w, r2)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed names
		}
		files.ServeHTTP(w, r)
	})
}
