package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/fiorix/go-web/autogzip"
	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru"
	"github.com/skia-dev/glog"
	"rsc.io/letsencrypt"
)

// flags
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

func LoggingGzipRequestResponse(h http.Handler) http.HandlerFunc {
	f := func(w http.ResponseWriter, r *http.Request) {
		incRef(r.URL.Path, r.Referer())
		h.ServeHTTP(w, r)
	}
	return autogzip.HandleFunc(f)
}

func main() {
	flag.Parse()
	defer glog.Flush()
	var err error
	if *sources == "" {
		*sources, err = os.Getwd()
		if err != nil {
			glog.Fatalf("Can't find working directory: %s", err)
		}
	}
	cache, err = lru.New(5000)
	if err != nil {
		glog.Fatalf("Failed to initialize log cache: %s", err)
	}

	r := mux.NewRouter()
	r.HandleFunc("/u/ref", refHandler)
	r.PathPrefix("/").HandlerFunc(makeStaticHandler())
	http.HandleFunc("/", LoggingGzipRequestResponse(r))

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
