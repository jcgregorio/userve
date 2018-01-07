package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fiorix/go-web/autogzip"
	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru"
	"github.com/jcgregorio/userve/go/mention"
	"github.com/skia-dev/glog"
	"go.skia.org/infra/go/ds"
	"go.skia.org/infra/go/httputils"
	"rsc.io/letsencrypt"
)

// flags
var (
	port         = flag.String("port", ":8000", "HTTP service address (e.g., ':8000')")
	sources      = flag.String("source", "", "The directory with the static resources to serve.")
	local        = flag.Bool("local", false, "Running locally, not on the server. If false this runs letsencrypt.")
	redirectFile = flag.String("redirect_file", "", "File of redirects, source and destination URL paths.")
)

func makeStaticHandler() func(http.ResponseWriter, *http.Request) {
	redir := map[string]string{}
	if *redirectFile != "" {
		b, err := ioutil.ReadFile(*redirectFile)
		if err != nil {
			glog.Errorf("Failed to read redirects file: %s", err)
		}
		lines := strings.Split(string(b), "\n")
		for _, line := range lines {
			parts := strings.Split(line, " ")
			if len(parts) != 2 {
				glog.Errorf("Failed to get redirect with just two parts: %v", parts)
				continue
			}
			redir[parts[0]] = parts[1]
		}
	}
	glog.Infof("Launching with %d redirects.", len(redir))
	fileServer := FileServer(*sources, redir)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "max-age=300")
		w.Header().Add("Link", "<https://bitworking.org/u/webmention>; rel=\"webmention\"")
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

func webmentionHandler(w http.ResponseWriter, r *http.Request) {
	m := mention.New(r.FormValue("source"), r.FormValue("target"))
	if err := m.FastValidate(); err != nil {
		glog.Infof("Invalid request: %s", err)
		http.Error(w, fmt.Sprintf("Invalid request: %s", err), 400)
		return
	}
	if err := mention.Put(r.Context(), m); err != nil {
		glog.Errorf("Failed to enqueue mention: %s", err)
		http.Error(w, fmt.Sprintf("Failed to enqueue mention"), 400)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func StartMentionRoutine(c *http.Client) {
	for _ = range time.Tick(time.Minute) {
		mention.VerifyQueuedMentions(c)
	}
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

	ds.Init("heroic-muse-88515", "blog")
	c := httputils.NewTimeoutClient()
	go StartMentionRoutine(c)

	r := mux.NewRouter()
	r.HandleFunc("/u/ref", refHandler)
	r.HandleFunc("/u/webmention", webmentionHandler)
	// TODO Endpoint with the latest.
	// TODO Endpoint that serves HTML of all approved.
	r.PathPrefix("/").HandlerFunc(makeStaticHandler())
	http.HandleFunc("/", LoggingGzipRequestResponse(r))

	if *local {
		glog.Fatal(http.ListenAndServe(*port, nil))
	} else {
		var m letsencrypt.Manager
		if err := m.CacheFile("/home/jcgregorio/letsencrypt.cache"); err != nil {
			glog.Fatal(err)
		}
		glog.Fatal(m.Serve())
	}
}
