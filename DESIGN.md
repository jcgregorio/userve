Micro-blogging
==============

  * Implements webmention.
  * Web UI for posting content and receiving mentions.
    * Autolinkify.
    * Maybe markdown?
  * Each blog page will have a small JS script that imports the approved
    webmentions for that page.
  * System also triggers outgoing webmentions.
  * Also for blog content by looking at the Atom feed.
  * Data for webmentions is stored in Google Cloud Datastore.
    * For each entry we need to store the list of approved and
      rejected webmentions, and if we sent webmentions for that URL yet.
    * Stored by target url, i.e. the bitworking.org permalink.

    type WebMebmention struct {
      Sent     bool
      Mentions []string
    }

Plan
----

  * Start with handling webmentions for the Atom feed.
  * Poll Atom feed.
    * Look up each Source [link] and [updated] value in GS.
    * If not present, or updated doesn't match, then emit webmentions for each Target
      found in the Source and write state to GS.
  * Serve webmention endpoint.
    * Collect unverified webmentions.
    * Periodically verify webmentions, at which point they become untriaged webmentions.
  * Serve web page for triaging webmentions.
  * Serve endpoint that displays approved webmentions for a given path.
    * Keep an lru cache of such results.
  * Add inline script to display approved webmentions to blog template.


