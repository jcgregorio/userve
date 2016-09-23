package atom

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	feed1 = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
   <title type="html">BitWorking</title>
   <link href="http://bitworking.org/" />
   <link href="http://bitworking.org/news/feed/" rel="self" />
   <link rel="hub" href="https://pubsubhubbub.appspot.com/"/>
   <link rel="me" href="http://www.google.com/profiles/joe.gregorio" type="text/html" />
   <updated>2016-09-12T07:21:48-04:00</updated>
   <author>
      <name>Joe Gregorio</name>
   </author>
   <id>http://bitworking.org/</id>
   <entry>
     <title type="html">Inertial Balance</title>
     <link href="http://bitworking.org/news/2016/08/interial_balance" />
     <id>http://bitworking.org/news/2016/08/content2</id>
     <updated>2016-08-16T22:42:54-04:00</updated>
     <content type="html">This is the content &lt;a href=&#34;http://example.com&#34;&gt;</content>
   </entry>
   <entry>
     <updated>2016-08-16T14:30:50-04:00</updated>
     <id>http://bitworking.org/news/2016/08/stuff</id>
     <content type="html">This is stuff</content>
   </entry>
</feed>`
)

func TestParse(t *testing.T) {
	// Valid.
	f, err := Parse([]byte(feed1))
	assert.NoError(t, err)
	assert.NotNil(t, f)
	assert.Equal(t, 2, len(f.Entry))
	expected := Feed{
		Entry: []Entry{
			Entry{
				ID:      "http://bitworking.org/news/2016/08/content2",
				Content: `This is the content <a href="http://example.com">`,
				Updated: "2016-08-16T22:42:54-04:00",
			},
			Entry{
				ID:      "http://bitworking.org/news/2016/08/stuff",
				Content: "This is stuff",
				Updated: "2016-08-16T14:30:50-04:00",
			},
		},
	}
	assert.Equal(t, expected.Entry[0], f.Entry[0])
	assert.Equal(t, expected.Entry[1], f.Entry[1])

	// Empty but well-formed.
	f, err = Parse([]byte(`<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	assert.NoError(t, err)
	assert.NotNil(t, f)

	// Malformed.
	f, err = Parse([]byte(""))
	assert.Error(t, err)
	assert.Nil(t, f)
}
