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
	port    = flag.String("port", ":8000", "HTTP service address (e.g., ':8000')")
	sources = flag.String("source", "", "The directory with the static resources to serve.")
	local   = flag.Bool("local", false, "Running locally, not on the server. If false this runs letsencrypt.")
)

func makeStaticHandler() func(http.ResponseWriter, *http.Request) {
	fileServer := FileServer(*sources)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "max-age=300")
		fileServer.ServeHTTP(w, r)
	}
}

func main() {
	flag.Parse()
	if *sources == "" {
		var err error
		*sources, err = os.Getwd()
		if err != nil {
			glog.Fatalf("Can't find working directory: %s", err)
		}
	}
	r := mux.NewRouter()
	r.PathPrefix("/").HandlerFunc(makeStaticHandler())
	http.Handle("/", httputils.LoggingGzipRequestResponse(r))

	if *local {
		glog.Fatal(http.ListenAndServe(*port, nil))
	} else {
		var m letsencrypt.Manager
		if err := m.CacheFile("letsencrypt.cache"); err != nil {
			glog.Fatal(err)
		}
		glog.Fatal(m.Serve())
	}
}
