package main

import (
	"net/http"
	"os"
	"path"
	"strings"
)

// Duplicate the code for http.FileServer, but add in the ability to serve HTML
// files w/o having the .html in the extension in the URL.
type fileHandler struct {
	dir string
}

// FileServer returns a handler that serves HTTP requests
// with the contents of the file system rooted at dir.
//
// As a special case, the returned file server redirects any request
// ending in "/index.html" to the same path, without the final
// "index.html".
func FileServer(dir string) http.Handler {
	return &fileHandler{
		dir: dir,
	}
}

func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upath := path.Join(f.dir, r.URL.Path)

	if finfo, err := os.Stat(upath); err == nil && finfo.IsDir() {
		index := strings.TrimSuffix(upath, "/") + "/index.atom"
		if _, err := os.Stat(index); err == nil {
			upath = index
		}
		w.Header().Set("Content-Type", "application/atom+xml")
	}
	if _, err := os.Stat(upath + ".html"); err == nil {
		upath += ".html"
	}

	http.ServeFile(w, r, path.Clean(upath))
}
