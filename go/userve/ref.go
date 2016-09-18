package main

import (
	"html/template"
	"net/http"
	"sort"

	lru "github.com/hashicorp/golang-lru"
	"github.com/skia-dev/glog"
)

type cacheEntry map[string]int

var (
	cache       *lru.Cache
	refTemplate *template.Template
	refSource   = `<!DOCTYPE html>
<html>
<head>
    <title></title>
    <meta charset="utf-8" />
    <meta http-equiv="X-UA-Compatible" content="IE=egde,chrome=1">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body>
  <dl>
  {{range .}}
    <dt>{{.Path}} - {{.Total}}</dt>
    <dd>
      <ul>
        {{range $url, $count :=   .Referrers}}
        <li>{{$url}} {{$count}} </li>
        {{end}}
      </ul>
    </dd>
  {{end}}
  </dl>
</body>
</html>
`
)

func incRef(path, referrer string) {
	glog.Infof("Request: %s %s", path, referrer)
	if referrer != "" {
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
		entry[referrer] = entry[referrer] + 1
	}
}

func init() {
	refTemplate = template.Must(template.New("ref").Parse(refSource))
}

type refSummary struct {
	Path      string
	Total     int
	Referrers map[string]int
}

type refSummarySlice []*refSummary

func (p refSummarySlice) Len() int           { return len(p) }
func (p refSummarySlice) Less(i, j int) bool { return p[i].Total > p[j].Total }
func (p refSummarySlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func refHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	ikeys := cache.Keys()
	summary := []*refSummary{}
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
	glog.Infof("%#v", summary)
	sort.Sort(refSummarySlice(summary))
	if err := refTemplate.Execute(w, summary); err != nil {
		glog.Errorf("Failed to render ref template: %s", err)
	}
}
