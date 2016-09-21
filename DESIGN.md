Micro-blogging
==============

  * Implements webmention.
  * Web UI for posting content and receiving mentions.
    * Autolinkify.
    * Maybe markdown?
  * Only approved mentions are displayed.
  * Handles mentions for both micro-content and the full blog.
  * Each blog page will have a small JS script that imports the approved
    webmentions for that page.
  * System also triggers outgoing webmentions for micro-content.
  * Also for blog content by looking at the Atom feed.
  * Data for micro-content is stored in Google Storage.
    * For each entry (blog or micro) we need to store the list of approved and
      rejected webmentions.
    * Stored by YYYY/MM/DD/[Unix Timestamp]
    * Can provide a listing by day or month using GS prefix queries.
    * YYYY/MM/DD/[Unix Timestamp].md is the content.
    * YYYY/MM/DD/[Unix Timestamp].json is the webmention state.
    * Similarly there is a blog/[some/blog/entry/path].json which stores each
      blog entries webmentions, also stores the state of outgoing webmentions,
      i.e. the 'updated' time of the entry when it's webmentions were
      triggered.
    * Untriaged webmentions are kept in a local JSON file.

Plan
----

Start with handling webmentions for the Atom feed.
Poll Atom feed.
  Look up each [link] and [updated] value in GS.
  If not present, or updated doesn't match, then emit webmentions and write state to GS.
Serve webmention endpoint.
  Collect unverified webmentions.
  Periodically verify webmentions, at which point they become untriaged webmentions.
Serve web page for triaging webmentions.
Serve endpoint that displays approved webmentions for a given path.
  Keep an lru cache of such results.
Add inline script to display approved webmentions to blog template.


