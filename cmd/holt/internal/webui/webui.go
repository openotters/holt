// Package webui owns the hub's embedded web console. The Vite/React
// build (a static SPA) lives under dist/; `task ui:build` populates it,
// and `go:embed all:dist` bakes it into the binary. A dist/.gitkeep
// placeholder keeps the embed valid on a fresh checkout that hasn't
// run the build.
//
// Serving modes mirror openotters:
//
//   - Embedded (default): files compiled in. Self-contained binary.
//   - Disk override (--ui-path DIR): serve a local build without
//     recompiling — useful while iterating on the UI.
//
// Both apply SPA fallback: any unknown, non-asset route resolves to
// index.html so client-side routing works on direct visits.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler returns an http.Handler serving the console. Empty diskPath
// uses the embedded build; otherwise files are read from disk.
func Handler(diskPath string) http.Handler {
	if diskPath != "" {
		return spaHandler(http.Dir(diskPath))
	}

	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return notBuiltHandler()
	}

	return spaHandler(http.FS(sub))
}

// spaHandler serves a Vite static build with SPA semantics: an exact
// file hit is served directly; asset paths (/assets/…) 404 honestly;
// everything else falls back to index.html.
func spaHandler(root http.FileSystem) http.HandlerFunc {
	server := http.FileServer(root)
	index, haveIndex := readFile(root, "index.html")

	return func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		if clean == "" || clean == "." {
			clean = "/"
		}

		if clean != "/" && isFile(root, clean) {
			server.ServeHTTP(w, r)

			return
		}

		// Never mask a missing asset with the SPA shell.
		if strings.HasPrefix(clean, "/assets/") || strings.Contains(path.Base(clean), ".") {
			http.NotFound(w, r)

			return
		}

		if !haveIndex {
			notBuiltHandler().ServeHTTP(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}
}

func isFile(root http.FileSystem, name string) bool {
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()

	return err == nil && !info.IsDir()
}

func readFile(root http.FileSystem, name string) ([]byte, bool) {
	f, err := root.Open(name)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)

	for {
		n, readErr := f.Read(tmp)
		buf = append(buf, tmp[:n]...)

		if readErr != nil {
			break
		}
	}

	return buf, len(buf) > 0
}

// notBuiltHandler explains that the UI hasn't been built yet.
func notBuiltHandler() http.Handler {
	const msg = "holt web console not built — run `task ui:build`, or start the hub without --ui\n"

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(msg))
	})
}
