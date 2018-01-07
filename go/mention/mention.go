package mention

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"go.skia.org/infra/go/ds"
	"go.skia.org/infra/go/util"
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
	GOOD_STATE   = "good"
	QUEUED_STATE = "queued"
	SPAM_STATE   = "spam"
)

type Mention struct {
	Source string
	Target string
	State  string
	TS     time.Time
}

func New(source, target string) *Mention {
	return &Mention{
		Source: source,
		Target: target,
		State:  QUEUED_STATE,
		TS:     time.Now(),
	}
}

func (m *Mention) key() string {
	return fmt.Sprintf("%x", md5.Sum([]byte(m.Source+m.Target)))
}

func (m *Mention) FastValidate() error {
	if m.Source == "" {
		return fmt.Errorf("Source is empty.")
	}
	if m.Target == "" {
		return fmt.Errorf("Target is empty.")
	}
	if m.Target == m.Source {
		return fmt.Errorf("Source and Target must be different.")
	}
	target, err := url.Parse(m.Target)
	if err != nil {
		return fmt.Errorf("Target is not a valid URL: %s", err)
	}
	if target.Hostname() != "bitworking.org" {
		return fmt.Errorf("Wrong target domain.")
	}
	if target.Scheme != "https" {
		return fmt.Errorf("Wrong scheme for target.")
	}
	return nil
}

func (m *Mention) SlowValidate(c *http.Client) error {
	resp, err := c.Get(m.Source)
	if err != nil {
		return fmt.Errorf("Failed to retrieve source: %s", err)
	}
	defer util.Close(resp.Body)
	links, err := webmention.DiscoverLinksFromReader(resp.Body, m.Source, "")
	if err != nil {
		return fmt.Errorf("Failed to discover links: %s", err)
	}
	for _, link := range links {
		if link == m.Target {
			return nil
		}
	}
	return fmt.Errorf("Failed to find target link in source.")
}

func VerifyQueuedMentions(c *http.Client) {
	queued := GetQueued(context.Background())
	for _, m := range queued {
		if m.SlowValidate(c) == nil {
			m.State = GOOD_STATE
		} else {
			m.State = SPAM_STATE
			glog.Warningf("Failed to validate webmention: %#v", *m)
		}
		if err := Put(context.Background(), m); err != nil {
			glog.Errorf("Failed to save validated message: %s", err)
		}
	}
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

func GetQueued(ctx context.Context) []*Mention {
	ret := []*Mention{}
	q := ds.NewQuery(MENTIONS).
		Filter("State =", QUEUED_STATE)

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

func Put(ctx context.Context, mention *Mention) error {
	key := ds.NewKey(MENTIONS)
	key.Name = mention.key()
	if _, err := ds.DS.Put(ctx, key, mention); err != nil {
		return fmt.Errorf("Failed writing %#v: %s", *mention, err)
	}
	return nil
}
