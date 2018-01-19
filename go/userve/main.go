package main

import (
	"encoding/json"
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

	units "github.com/docker/go-units"
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
	mentionsTemplate = template.Must(template.New("mentions").Funcs(template.FuncMap{
		"humanTime": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return " • " + units.HumanDuration(time.Now().Sub(t)) + " ago"
		},
		"rfc3999": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(time.RFC3339)
		},
	}).Parse(`
	<section id=webmention>
	<h3>WebMentions</h3>
	{{ range . }}
	    <span class="wm-author">
				{{ if .AuthorURL }}
					{{ if .Thumbnail }}
					<a href="{{ .AuthorURL}}" rel=nofollow class="wm-thumbnail">
						<img src="/u/thumbnail/{{ .Thumbnail }}"/>
					</a>
					{{ end }}
					<a href="{{ .AuthorURL}}" rel=nofollow>
						{{ .Author }}
					</a>
				{{ else }}
					{{ .Author }}
				{{ end }}
			</span>
			<time datetime="{{ .Published | rfc3999 }}">{{ .Published | humanTime }}</time>
			<a class="wm-content" href="{{ .Source }}" rel=nofollow>
				{{ if .Title }}
					{{ .Title }}
				{{ else }}
					{{ .Source }}
				{{ end }}
			</a>
	{{ end }}
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
		<style type="text/css" media="screen">
		  #webmentions {
				display: grid;
				padding: 1em;
				grid-template-columns: 5em 1fr 1fr 1fr;
				grid-column-gap: 10px;
				grid-row-gap: 6px;
			}
		</style>
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
  <div id=webmentions>
  {{range .Mentions }}
		<select name="text" data-key="{{ .Key }}">
			<option value="good" {{if eq .State "good" }}selected{{ end }} >Good</option>
			<option value="spam" {{if eq .State "spam" }}selected{{ end }} >Spam</option>
			<option value="untriaged" {{if eq .State "untriaged" }}selected{{ end }} >Untriaged</option>
		</select>
		<a href="{{ .Source }}">{{ .Source }}</a>
		<a href="{{ .Target }}">{{ .Target }}</a>
		<span>{{ .TS | humanTime }}</span>
  {{end}}
  </div>
	<div><a href="/u/triage?offset={{.Offset}}">Next</a></div>
	<script type="text/javascript" charset="utf-8">
	 // TODO - listen on div.webmentions for click/input and then write
	 // triage action back to server.
	 document.getElementById('webmentions').addEventListener('change', e => {
		 console.log(e);
		 if (e.target.dataset.key != "") {
			 fetch("/u/updateMention", {
				 method: 'POST',
				 body: JSON.stringify({
					 key: e.target.dataset.key,
					 value:  e.target.value,
				 }),
				 headers: new Headers({
					 'Content-Type': 'application/json'
				 })
			 }).catch(e => console.error('Error:', e));
		 }
	 });
	</script>
</body>
</html>`, CLIENT_ID)
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
	return autogzip.HandleFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "max-age=300")
		w.Header().Add("Link", "<https://bitworking.org/u/webmention>; rel=\"webmention\"")
		fileServer.ServeHTTP(w, r)
	})
}

func LoggingRequestResponse(h http.Handler) http.HandlerFunc {
	f := func(w http.ResponseWriter, r *http.Request) {
		incRef(r.URL.Path, r.Referer())
		h.ServeHTTP(w, r)
	}
	return f
}

type UpdateMention struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func updateTriageHandler(w http.ResponseWriter, r *http.Request) {
	isAdmin := *local || isAdmin(r)
	if !isAdmin {
		http.Error(w, "Unauthorized", 401)
	}
	var u UpdateMention
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		glog.Errorf("Failed to decode update: %s", err)
		http.Error(w, "Bad JSON", 400)
	}
	if err := mention.UpdateState(r.Context(), u.Key, u.Value); err != nil {
		glog.Errorf("Failed to write update: %s", err)
		http.Error(w, "Failed to write", 400)
	}
}

func thumbnailHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	vars := mux.Vars(r)
	b, err := mention.GetThumbnail(r.Context(), vars["id"])
	if err != nil {
		http.Error(w, "Image not found", 404)
		glog.Errorf("Failed to get image: %s", err)
		return
	}
	if _, err = w.Write(b); err != nil {
		glog.Errorf("Failed to write image: %s", err)
		return
	}
}

// mentionsHandler returns HTML describing all the good Webmentions for the given URL.
func mentionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	m := mention.GetGood(r.Context(), r.Referer())
	if len(m) == 0 {
		return
	}
	if err := mentionsTemplate.Execute(w, m); err != nil {
		glog.Errorf("Failed to expand template: %s", err)
	}
}

// webmentionHandler handles incoming Webmentions.
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
	Mentions []*mention.MentionWithKey
	Offset   int64
}

// triageHandler displays the triage page for Webmentions.
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
			Offset:   offset + limit,
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
	triageTemplate = template.Must(template.New("triage").Funcs(template.FuncMap{
		"humanTime": func(t time.Time) string {
			return units.HumanDuration(time.Now().Sub(t)) + " ago."
		},
	}).Parse(triageSource))
	cache, err = lru.New(5000)
	if err != nil {
		glog.Fatalf("Failed to initialize log cache: %s", err)
	}

	ds.Init("heroic-muse-88515", "blog")
	c := httputils.NewTimeoutClient()
	go StartMentionRoutine(c)
	go StartAtomMonitor(c)

	r := mux.NewRouter()
	u := r.PathPrefix("/u").Subrouter()
	u.HandleFunc("/ref", refHandler)
	u.HandleFunc("/webmention", webmentionHandler).Methods("POST")
	u.HandleFunc("/mentions", mentionsHandler)
	u.HandleFunc("/triage", triageHandler)
	u.HandleFunc("/updateMention", updateTriageHandler)
	u.HandleFunc("/thumbnail/{id:[a-z0-9]+}", thumbnailHandler)

	r.PathPrefix("/").HandlerFunc(makeStaticHandler())
	http.HandleFunc("/", LoggingRequestResponse(r))

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
