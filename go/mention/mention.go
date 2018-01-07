package mention

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	"go.skia.org/infra/go/ds"
	"google.golang.org/api/iterator"
	"willnorris.com/go/webmention"

	"github.com/jcgregorio/userve/go/atom"
	"github.com/skia-dev/glog"
)

const (
	MENTIONS ds.Kind = "Mentions"
)

type MentionSource struct {
	Targets []string
	Updated time.Time
}

func ParseAtomFeed(r io.Reader) (map[string]*MentionSource, error) {
	ret := map[string]*MentionSource{}
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
		ret[entry.Link.HREF] = &MentionSource{
			Targets: links,
			Updated: updated,
		}
	}
	return ret, nil
}

const (
	GOOD_STATE = "good"
	SPAM_STATE = "spam"
)

type Mention struct {
	Source string
	Target string
	State  string
	TS     time.Time
}

func (m *Mention) key() string {
	return fmt.Sprintf("%x", md5.Sum([]byte(m.Source+m.Target)))
}

func get(ctx context.Context, target string, all bool) []*Mention {
	ret := []*Mention{}
	q := ds.NewQuery(MENTIONS).
		Filter("Target =", target)
	if !all {
		q = q.Filter("State =", GOOD_STATE)
	}

	it := ds.DS.Run(ctx, q)
	for {
		m := &Mention{}
		_, err := it.Next(m)
		if err == iterator.Done {
			break
		}
		if err != nil {
			glog.Errorf("Failed while reading: %s", err)
			break
		}
		ret = append(ret, m)
	}
	return ret
}

func GetAll(ctx context.Context, target string) []*Mention {
	return get(ctx, target, true)
}

func GetGood(ctx context.Context, target string) []*Mention {
	return get(ctx, target, false)
}

func Put(ctx context.Context, mention *Mention) error {
	key := ds.NewKey(MENTIONS)
	key.Name = mention.key()
	if _, err := ds.DS.Put(ctx, key, mention); err != nil {
		return fmt.Errorf("Failed writing %#v: %s", *mention, err)
	}
	return nil
}
