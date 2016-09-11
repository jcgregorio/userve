package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/skia-dev/glog"

	"go.skia.org/infra/go/httputils"
  "rsc.io/letsencrypt"
)

var (
	port         = flag.String("port", ":8000", "HTTP service address (e.g., ':8000')")
	resourcesDir = flag.String("resources_dir", "", "The directory to find templates, JS, and CSS files. If blank the current directory will be used.")
)

func makeResourceHandler() func(http.ResponseWriter, *http.Request) {
	fileServer := http.FileServer(http.Dir(*resourcesDir))
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "max-age=300")
		fileServer.ServeHTTP(w, r)
	}
}

func main() {
	flag.Parse()
	if *resourcesDir == "" {
		var err error
		*resourcesDir, err = os.Getwd()
		if err != nil {
			glog.Fatalf("Can't find working directory: %s", err)
		}
	}
	r := mux.NewRouter()
	r.PathPrefix("/").HandlerFunc(makeResourceHandler())
	http.Handle("/", httputils.LoggingGzipRequestResponse(r))

  var m letsencrypt.Manager
  if err := m.CacheFile("letsencrypt.cache"); err != nil {
    glog.Fatal(err)
  }
  glog.Fatal(m.Serve())
}
