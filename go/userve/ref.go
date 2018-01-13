package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/skia-dev/glog"
	"go.skia.org/infra/go/httputils"
)

type cacheEntry map[string]int

const (
	CLIENT_ID = "952643138919-jh0117ivtbqkc9njoh91csm7s465c4na.apps.googleusercontent.com"

	NO_REFERRER = "∅"
)

var (
	cache       *lru.Cache
	refTemplate *template.Template
	refSource   = fmt.Sprintf(`<!DOCTYPE html>
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
  <dl>
  {{range .Summary}}
    <details>
      <summary>{{.Path}} - {{.Total}}</summary>
      <ul>
        {{range $url, $count :=   .Referrers}}
        <li>{{$url}} {{$count}} </li>
        {{end}}
      </ul>
    </details>
  {{end}}
  </dl>
</body>
</html>
`, CLIENT_ID)
	client *http.Client
)

func incRef(path, referrer string) {
	glog.Infof("Request: %s %s", path, referrer)
	if strings.HasPrefix(referrer, "https://bitworking.org") {
		return
	}
	var entry cacheEntry
	ientry, ok := cache.Get(path)
	if !ok {
		entry = cacheEntry{}
		cache.Add(path, entry)
	} else {
		entry, ok = ientry.(cacheEntry)
		if !ok {
			glog.Error("Wrong thing in cache?: %v", ientry)
			return
		}
	}
	if referrer != "" {
		entry[referrer] = entry[referrer] + 1
	} else {
		entry[NO_REFERRER] = entry[NO_REFERRER] + 1
	}
}

func init() {
	refTemplate = template.Must(template.New("ref").Parse(refSource))
	client = httputils.NewTimeoutClient()
	go func() {
		for _ = range time.Tick(time.Hour * 24) {
			cache.Purge()
		}
	}()
}

type Claims struct {
	Mail    string `json:"email"`
	Aud     string `json:"aud"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func isAdmin(r *http.Request) bool {
	idtoken, err := r.Cookie("id_token")
	if err != nil {
		glog.Info("Cookie not set.")
		return false
	}
	resp, err := client.Get(fmt.Sprintf("https://www.googleapis.com/oauth2/v3/tokeninfo?%s", idtoken))
	if err != nil || resp.StatusCode != 200 {
		glog.Errorf("Failed to validate idtoken: %#v %s", *resp, err)
		return false
	}
	claims := Claims{}
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		glog.Errorf("Failed to decode claims: %s", err)
		return false
	}
	// Check if aud is correct.
	if claims.Aud != CLIENT_ID {
		return false
	}

	return claims.Mail == "joe.gregorio@gmail.com"
}

type refSummary struct {
	Path      string
	Total     int
	Referrers map[string]int
}

type refPageContext struct {
	IsAdmin bool
	Summary []*refSummary
}

type refSummarySlice []*refSummary

func (p refSummarySlice) Len() int           { return len(p) }
func (p refSummarySlice) Less(i, j int) bool { return p[i].Total > p[j].Total }
func (p refSummarySlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func refHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	ikeys := cache.Keys()
	summary := []*refSummary{}
	isAdmin := *local || isAdmin(r)
	if isAdmin {
		for _, ik := range ikeys {
			key := ik.(string)
			irefs, ok := cache.Get(ik)
			if !ok {
				glog.Error("No cache hit?: %v", irefs)
				continue
			}
			refs, ok := irefs.(cacheEntry)
			if !ok {
				glog.Error("Wrong thing in cache?: %v", irefs)
				continue
			}
			total := 0
			for _, n := range refs {
				total += n
			}
			summary = append(summary, &refSummary{
				Path:      key,
				Total:     total,
				Referrers: refs,
			})
		}
	}
	sort.Sort(refSummarySlice(summary))
	if err := refTemplate.Execute(w, refPageContext{
		IsAdmin: isAdmin,
		Summary: summary,
	}); err != nil {
		glog.Errorf("Failed to render ref template: %s", err)
	}
}
