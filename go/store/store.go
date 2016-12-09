// Package store provides for loading and saving webmention state.
package store

import (
	"fmt"
	"net/http"

	"cloud.google.com/go/storage"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
)

type Store struct {
	bucket *storage.Bucket
}

func New(c *http.Client, bucket string) (*Store, error) {
	client, err := storage.NewClient(context.Background(), option.WithHTTPClient(c))
	if err != nil {
		return nil, fmt.Errorf("Failed to create a Google Storage client: %s", err)
	}
	return &Store{
		bucket: client.Bucket(bucket),
	}
}

type Sent struct {
	Status     string
	StatusCode int
}

// WebMentions stores the status of both incoming and outgoing webmentions for
// a given URL.
type WebMentions struct {
	Sent     map[string]Sent
	Received map[string]bool
}

// Save the given WebMention for the given URL. The write will not succeed
// if the generation doesn't match, thus avoiding the lost update problem.
func (s *Store) Save(urlStr string, w *WebMentions, gen int64) error {
	// Parse URL, extract path.
}

// Load the WebMentions for the given URL and the GS object generation.
func (s *Store) Load(urlStr string) (*WebMentions, int64, error) {
}
