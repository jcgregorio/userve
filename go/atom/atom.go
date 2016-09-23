// Package atom reads an Atom feed.
package atom

import "encoding/xml"

type Feed struct {
	Entry []Entry `xml:"entry"`
}

type Entry struct {
	ID      string `xml:"id"`
	Content string `xml:"content"`
	Updated string `xml:"updated"`
}

func Parse(b []byte) (*Feed, error) {
	ret := &Feed{
		Entry: []Entry{},
	}
	if err := xml.Unmarshal(b, ret); err != nil {
		return nil, err
	}
	return ret, nil
}
