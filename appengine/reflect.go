package reflect

import (
	"io"
	"net/http"
	"net/url"

	"appengine"
	"appengine/urlfetch"
)

const forwardURL = "http://tor1.bamsoftware.com:7002/"

func pathJoin(a, b string) string {
	if len(a) > 0 && a[len(a)-1] == '/' {
		a = a[:len(a)-1]
	}
	if len(b) == 0 || b[0] != '/' {
		b = "/" + b
	}
	return a + b
}

// We reflect only a whitelisted set of header fields. In requests, the full
// list includes things like User-Agent and X-Appengine-Country that the Tor
// bridge doesn't need to know. In responses, there may be things like
// Transfer-Encoding that interfere with App Engine's own hop-by-hop headers.
var reflectedHeaderFields = []string{
	"Content-Type",
	"X-Session-Id",
}

func copyRequest(r *http.Request) (*http.Request, error) {
	u, err := url.Parse(forwardURL)
	if err != nil {
		return nil, err
	}
	u.Path = pathJoin(u.Path, r.URL.Path)
	c, err := http.NewRequest(r.Method, u.String(), r.Body)
	if err != nil {
		return nil, err
	}
	for _, key := range reflectedHeaderFields {
		value := r.Header.Get(key)
		if value != "" {
			c.Header.Add(key, value)
		}
	}
	return c, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	client := urlfetch.Client(appengine.NewContext(r))
	fr, err := copyRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := client.Transport.RoundTrip(fr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, key := range reflectedHeaderFields {
		value := resp.Header.Get(key)
		if value != "" {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func init() {
	http.HandleFunc("/", handler)
}
