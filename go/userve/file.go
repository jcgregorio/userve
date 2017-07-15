package main

import (
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/skia-dev/glog"
)

// Duplicate the code for http.FileServer, but add in the ability to serve HTML
// files w/o having the .html in the extension in the URL.
type fileHandler struct {
	dir       string
	redirects map[string]string
}

// FileServer returns a handler that serves HTTP requests
// with the contents of the file system rooted at dir.
//
// As a special case, the returned file server redirects any request
// ending in "/index.html" to the same path, without the final
// "index.html".
func FileServer(dir string, redirects map[string]string) http.Handler {
	return &fileHandler{
		dir:       dir,
		redirects: redirects,
	}
}

func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Path: %q", r.URL.Path)
	upath := path.Join(f.dir, r.URL.Path)
	if newpath, ok := f.redirects[r.URL.Path]; ok {
		glog.Infof("NewPath: %q", newpath)
		upath = path.Join(f.dir, newpath)
	}

	if finfo, err := os.Stat(upath); err == nil && finfo.IsDir() {
		index := strings.TrimSuffix(upath, "/") + "/index.atom"
		if _, err := os.Stat(index); err == nil {
			upath = index
			w.Header().Set("Content-Type", "application/atom+xml")
		}
	}
	if _, err := os.Stat(upath + ".html"); err == nil {
		upath += ".html"
	}

	http.ServeFile(w, r, path.Clean(upath))
}
