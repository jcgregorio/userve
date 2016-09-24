package mention

import (
	"bytes"
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
     <link href="http://bitworking.org/news/2016/08/stuff"/>
     <content type="html">This is stuff</content>
   </entry>
   <entry>
     <updated>2016-08-16T14:30:50-04:00</updated>
     <id>http://bitworking.org/news/2016/09/relative</id>
     <link href="http://bitworking.org/news/2016/08/relative"/>
     <content type="html">This is the content &lt;a href=&#34;sample.js&#34;&gt;</content>
   </entry>
</feed>`
)

func TestParseAtomFeed(t *testing.T) {
	buf := bytes.NewBufferString(feed1)
	mentionSources, err := ParseAtomFeed(buf)
	assert.NoError(t, err)
	assert.NotNil(t, mentionSources)
	assert.Equal(t, 3, len(mentionSources))
	assert.Equal(t, "http://example.com", mentionSources[0].Targets[0])
	assert.Equal(t, "http://bitworking.org/news/2016/08/interial_balance", mentionSources[0].Source)
	assert.Equal(t, 0, len(mentionSources[1].Targets))
	assert.Equal(t, "http://bitworking.org/news/2016/08/sample.js", mentionSources[2].Targets[0])
	assert.Equal(t, "http://bitworking.org/news/2016/08/sample.js", mentionSources[2].Targets[0])
}
