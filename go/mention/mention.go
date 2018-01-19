package mention

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
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
	"willnorris.com/go/microformats"
	"willnorris.com/go/webmention"

	"github.com/jcgregorio/userve/go/atom"
	"github.com/nfnt/resize"
	"github.com/skia-dev/glog"
)

const (
	MENTIONS         ds.Kind = "Mentions"
	WEB_MENTION_SENT ds.Kind = "WebMentionSent"
	THUMBNAIL        ds.Kind = "Thumbnail"
)

type WebMentionSent struct {
	TS time.Time
}

func sent(source string) (time.Time, bool) {
	key := ds.NewKey(WEB_MENTION_SENT)
	key.Name = source

	dst := &WebMentionSent{}
	if err := ds.DS.Get(context.Background(), key, dst); err != nil {
		glog.Warningf("Failed to find source: %q", source)
		return time.Time{}, false
	} else {
		glog.Warningf("Found source: %q", source)
		return dst.TS, true
	}
}

func recordSent(source string, updated time.Time) error {
	key := ds.NewKey(WEB_MENTION_SENT)
	key.Name = source

	src := &WebMentionSent{
		TS: updated.UTC(),
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
		ts, ok := sent(source)
		glog.Warningf("Updated: %v  ts: %v ok: %v after: %v", ms.Updated.Unix(), ts.Unix(), ok, ms.Updated.After(ts.Add(time.Second)))
		if ok && ts.Before(ms.Updated.Add(time.Second)) {
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
		if err := recordSent(source, ms.Updated); err != nil {
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

	// Metadata found when validating. We might display this.
	Title     string    `datastore:",noindex"`
	Author    string    `datastore:",noindex"`
	AuthorURL string    `datastore:",noindex"`
	Published time.Time `datastore:",noindex"`
	Thumbnail string    `datastore:",noindex"`
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
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to read content: %s", err)
	}
	reader := bytes.NewReader(b)
	links, err := webmention.DiscoverLinksFromReader(reader, m.Source, "")
	if err != nil {
		return fmt.Errorf("Failed to discover links: %s", err)
	}
	for _, link := range links {
		if link == m.Target {
			_, err := reader.Seek(0, io.SeekStart)
			if err != nil {
				return nil
			}
			m.ParseMicroformats(reader, MakeUrlToImageReader(c))
			return nil
		}
	}
	return fmt.Errorf("Failed to find target link in source.")
}

func (m *Mention) ParseMicroformats(r io.Reader, urlToImageReader UrlToImageReader) {
	u, err := url.Parse(m.Source)
	if err != nil {
		return
	}
	data := microformats.Parse(r, u)
	findHEntry(context.Background(), urlToImageReader, m, data.Items)
	// Find an h-entry with the m.Target.
}

func VerifyQueuedMentions(c *http.Client) {
	queued := GetQueued(context.Background())
	for _, m := range queued {
		glog.Infof("Verifying queued webmention from %q", m.Source)
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
	// TODO See if there's an existing mention already, so we don't overwrite its status?
	key := ds.NewKey(MENTIONS)
	key.Name = mention.key()
	if _, err := ds.DS.Put(ctx, key, mention); err != nil {
		return fmt.Errorf("Failed writing %#v: %s", *mention, err)
	}
	return nil
}

type UrlToImageReader func(url string) (io.ReadCloser, error)

func in(s string, arr []string) bool {
	for _, a := range arr {
		if a == s {
			return true
		}
	}
	return false
}

func firstPropAsString(uf *microformats.Microformat, key string) string {
	for _, sint := range uf.Properties[key] {
		if s, ok := sint.(string); ok {
			return s
		}
	}
	return ""
}

func findHEntry(ctx context.Context, u2r UrlToImageReader, m *Mention, items []*microformats.Microformat) {
	for _, it := range items {
		if in("h-entry", it.Type) {
			entryURL := firstPropAsString(it, "url")
			if entryURL != "" && entryURL != m.Source {
				return
			}
			m.Title = firstPropAsString(it, "name")
			if t, err := time.Parse(time.RFC3339, firstPropAsString(it, "published")); err == nil {
				m.Published = t
			}
			if authorsInt, ok := it.Properties["author"]; ok {
				for _, authorInt := range authorsInt {
					if author, ok := authorInt.(*microformats.Microformat); ok {
						findAuthor(ctx, u2r, m, author)
					}
				}
			}
		}
		findHEntry(ctx, u2r, m, it.Children)
	}
}

type Thumbnail struct {
	PNG []byte `datastore:",noindex"`
}

func MakeUrlToImageReader(c *http.Client) UrlToImageReader {
	return func(u string) (io.ReadCloser, error) {
		resp, err := c.Get(u)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving thumbnail: %s", err)
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("Not a 200 response: %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

func findAuthor(ctx context.Context, u2r UrlToImageReader, m *Mention, it *microformats.Microformat) {
	glog.Info("Found author in microformat.")
	m.Author = it.Value
	m.AuthorURL = firstPropAsString(it, "url")
	u := firstPropAsString(it, "photo")
	if u == "" {
		glog.Warning("No photo URL found.")
		return
	}

	r, err := u2r(u)
	if err != nil {
		glog.Warning("Failed to retrieve photo.")
		return
	}

	defer util.Close(r)
	img, _, err := image.Decode(r)
	if err != nil {
		glog.Warning("Failed to decode photo.")
		return
	}
	rect := img.Bounds()
	var x uint = 32
	var y uint = 32
	if rect.Max.X > rect.Max.Y {
		y = 0
	} else {
		x = 0
	}
	resized := resize.Resize(x, y, img, resize.Lanczos3)

	var buf bytes.Buffer
	encoder := png.Encoder{
		CompressionLevel: png.BestCompression,
	}
	if err := encoder.Encode(&buf, resized); err != nil {
		glog.Warning("Failed to encode photo.")
		return
	}

	hash := fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
	t := &Thumbnail{
		PNG: buf.Bytes(),
	}
	key := ds.NewKey(THUMBNAIL)
	key.Name = hash
	if _, err := ds.DS.Put(ctx, key, t); err != nil {
		glog.Errorf("Failed to write: %s", err)
		return
	}
	m.Thumbnail = hash
}
