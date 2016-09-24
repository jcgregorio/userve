package mention

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	"willnorris.com/go/webmention"

	"github.com/jcgregorio/userve/go/atom"
	"github.com/skia-dev/glog"
)

type MentionSource struct {
	Links   []string
	ID      string
	Updated time.Time
}

func ParseAtomFeed(r io.Reader) ([]MentionSource, error) {
	ret := []MentionSource{}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("Failed to read feed: %s", err)
	}
	feed, err := atom.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse feed: %s", err)
	}
	for _, entry := range feed.Entry {
		buf := bytes.NewBufferString(entry.Content)
		links, err := webmention.DiscoverLinksFromReader(buf, entry.Link.HREF, "")
		if err != nil {
			glog.Errorf("Failed while discovering links in %q: %s", entry.Link.HREF, err)
			continue
		}
		updated, err := time.Parse(time.RFC3339, entry.Updated)
		if err != nil {
			fmt.Errorf("Failed to parse entry timestamp: %s", err)
		}
		ret = append(ret, MentionSource{
			Links:   links,
			ID:      entry.ID,
			Updated: updated,
		})
	}
	return ret, nil
}
