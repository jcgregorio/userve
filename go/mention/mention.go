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
	"os"
	"time"

	"cloud.google.com/go/datastore"
	"go.skia.org/infra/go/ds"
	"go.skia.org/infra/go/util"
	"google.golang.org/api/iterator"
	"willnorris.com/go/webmention"

	"github.com/jcgregorio/userve/go/atom"
	"github.com/skia-dev/glog"
)

const (
	MENTIONS         ds.Kind = "Mentions"
	WEB_MENTION_SENT ds.Kind = "WebMentionSent"
)

type WebMentionSent struct {
	TS time.Time
}

func sent(source string) (time.Time, bool) {
	key := ds.NewKey(WEB_MENTION_SENT)
	key.Name = source

	dst := &WebMentionSent{}
	if err := ds.DS.Get(context.Background(), key, dst); err != nil {
		return time.Time{}, false
	} else {
		return dst.TS, true
	}
}

func recordSent(source string) error {
	key := ds.NewKey(WEB_MENTION_SENT)
	key.Name = source

	src := &WebMentionSent{
		TS: time.Now().UTC(),
	}
	_, err := ds.DS.Put(context.Background(), key, src)
	return err
}

func ProcessAtomFeed(c *http.Client, filename string) error {
	glog.Info("Processing Atom Feed")
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer util.Close(f)
	mentionSources, err := ParseAtomFeed(f)
	if err != nil {
		return err
	}
	wmc := webmention.New(c)
	for source, ms := range mentionSources {
		if ts, ok := sent(source); ok && !ms.Updated.After(ts) {
			glog.Infof("Skipping since already sent: %s", source)
			continue
		}
		glog.Infof("Processing Source: %s", source)
		for _, target := range ms.Targets {
			glog.Infof("  to Target: %s", target)
			endpoint, err := wmc.DiscoverEndpoint(target)
			if err != nil {
				glog.Errorf("Failed looking for endpoint: %s", err)
				continue
			} else if endpoint == "" {
				glog.Infof("No webmention support at: %s", target)
				continue
			}
			_, err = wmc.SendWebmention(endpoint, source, target)
			if err != nil {
				glog.Errorf("Error sending webmention to %s: %s", target, err)
			} else {
				glog.Infof("Sent webmention from %s to %s", source, target)
			}
		}
		if err := recordSent(source); err != nil {
			glog.Errorf("Failed recording Sent state: %s", err)
		}
	}
	return nil
}

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
	GOOD_STATE      = "good"
	UNTRIAGED_STATE = "untriaged"
	SPAM_STATE      = "spam"
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
		State:  UNTRIAGED_STATE,
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

func UpdateState(ctx context.Context, encodedKey, state string) error {
	tx, err := ds.DS.NewTransaction(ctx)
	if err != nil {
		return fmt.Errorf("client.NewTransaction: %v", err)
	}
	key, err := datastore.DecodeKey(encodedKey)
	if err != nil {
		return fmt.Errorf("Unable to decode key: %s", err)
	}
	var m Mention
	if err := tx.Get(key, &m); err != nil {
		tx.Rollback()
		return fmt.Errorf("tx.GetMulti: %v", err)
	}
	m.State = state
	if _, err := tx.Put(key, &m); err != nil {
		tx.Rollback()
		return fmt.Errorf("tx.Put: %v", err)
	}
	if _, err = tx.Commit(); err != nil {
		return fmt.Errorf("tx.Commit: %v", err)
	}
	return nil
}

type MentionWithKey struct {
	Mention
	Key string
}

func GetTriage(ctx context.Context, limit, offset int) []*MentionWithKey {
	ret := []*MentionWithKey{}
	q := ds.NewQuery(MENTIONS).Order("-TS").Limit(limit).Offset(offset)

	it := ds.DS.Run(ctx, q)
	for {
		var m Mention
		key, err := it.Next(&m)
		if err == iterator.Done {
			break
		}
		if err != nil {
			glog.Errorf("Failed while reading: %s", err)
			break
		}
		ret = append(ret, &MentionWithKey{
			Mention: m,
			Key:     key.Encode(),
		})
	}
	return ret
}

func GetQueued(ctx context.Context) []*Mention {
	ret := []*Mention{}
	q := ds.NewQuery(MENTIONS).
		Filter("State =", UNTRIAGED_STATE)

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
