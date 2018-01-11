package main

import (
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
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

var (
	mentionsTemplate = template.Must(template.New("mentions").Parse(`
	<section>
	<h3>WebMentions</h3>
	<ul>
	{{ range . }}
    <a href="{{ .Source }}" rel=nofollow>{{ .Source }}</a>
	{{ end }}
	</ul>
	</section>
`))

	triageTemplate *template.Template
	triageSource   = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title></title>
    <meta charset="utf-8" />
    <meta http-equiv="X-UA-Compatible" content="IE=egde,chrome=1">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="google-signin-scope" content="profile email">
    <meta name="google-signin-client_id" content="%s">
    <script src="https://apis.google.com/js/platform.js" async defer></script>
</head>
<body>
  <div class="g-signin2" data-onsuccess="onSignIn" data-theme="dark"></div>
    <script>
      function onSignIn(googleUser) {
        document.cookie = "id_token=" + googleUser.getAuthResponse().id_token;
        if (!{{.IsAdmin}}) {
          window.location.reload();
        }
      };
    </script>
  <ul>
  {{range .Mentions }}
	  <li>
		  <a href="{{ .Source }}">{{ .Source }}</a>  <a href="{{ .Target }}">{{ .Target }}<a/> {{ .State }}
	  </li>
  {{end}}
  </ul>
</body>
</html>
`)
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

func mentionsHandler(w http.ResponseWriter, r *http.Request) {
	m := mention.GetGood(r.Context(), r.Referer())
	if len(m) == 0 {
		return
	}
	if err := mentionsTemplate.Execute(w, m); err != nil {
		glog.Errorf("Failed to expand template: %s", err)
	}
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

func StartAtomMonitor(c *http.Client) {
	lastModified := time.Time{}
	filename := path.Join(*sources, "news", "feed", "index.atom")
	for _ = range time.Tick(time.Minute) {
		glog.Info("Checking Atom Feed")
		st, err := os.Stat(filename)
		if err != nil {
			glog.Errorf("Failed to stat Atom feed: %s", err)
			continue
		}
		if st.ModTime().After(lastModified) {
			lastModified = st.ModTime()
			if err := mention.ProcessAtomFeed(c, filename); err != nil {
				glog.Errorf("Failed to process Atom feed: %s", err)
			}
		} else {
			glog.Info("Atom Feed Unmodified.")
		}
	}
}

type triageContext struct {
	IsAdmin  bool
	Mentions []*mention.Mention
}

func triageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	context := &triageContext{}
	isAdmin := *local || isAdmin(r)
	if isAdmin {
		limitText := r.FormValue("limit")
		if limitText == "" {
			limitText = "20"
		}
		offsetText := r.FormValue("offset")
		if offsetText == "" {
			offsetText = "0"
		}
		limit, err := strconv.ParseInt(limitText, 10, 32)
		if err != nil {
			glog.Errorf("Failed to parse limit: %s", err)
			return
		}
		offset, err := strconv.ParseInt(offsetText, 10, 32)
		if err != nil {
			glog.Errorf("Failed to parse offset: %s", err)
			return
		}
		context = &triageContext{
			IsAdmin:  isAdmin,
			Mentions: mention.GetTriage(r.Context(), int(limit), int(offset)),
		}
	}
	if err := triageTemplate.Execute(w, context); err != nil {
		glog.Errorf("Failed to render triage template: %s", err)
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
	triageTemplate = template.Must(template.New("triage").Parse(triageSource))
	cache, err = lru.New(5000)
	if err != nil {
		glog.Fatalf("Failed to initialize log cache: %s", err)
	}

	ds.Init("heroic-muse-88515", "blog")
	c := httputils.NewTimeoutClient()
	go StartMentionRoutine(c)
	go StartAtomMonitor(c)

	r := mux.NewRouter()
	r.HandleFunc("/u/ref", refHandler)
	r.HandleFunc("/u/webmention", webmentionHandler)

	r.HandleFunc("/u/mentions", mentionsHandler)
	r.HandleFunc("/u/triage", triageHandler)
	// TODO Endpoint with the latest.
	r.PathPrefix("/").HandlerFunc(makeStaticHandler())
	http.HandleFunc("/", LoggingGzipRequestResponse(r))

	// TODO Also do login and handle comments.

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
