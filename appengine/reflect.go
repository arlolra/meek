package reflect

import (
	"io"
	"net/http"
	"net/url"
	"time"

	"appengine"
	"appengine/urlfetch"
)

const forwardURL = "http://meek.bamsoftware.com:7002/"
// A timeout of 0 means to use the App Engine default (5 seconds).
const urlFetchTimeout = 10 * time.Second
var context appengine.Context

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
	context = appengine.NewContext(r)
	fr, err := copyRequest(r)
	if err != nil {
		context.Errorf("copyRequest: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	transport := urlfetch.Transport{
		Context: context,
		// Despite the name, Transport.Deadline is really a timeout and
		// not an absolute deadline as used in the net package. In
		// other words it is a time.Duration, not a time.Time.
		Deadline: urlFetchTimeout,
	}
	resp, err := transport.RoundTrip(fr)
	if err != nil {
		context.Errorf("RoundTrip: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	for _, key := range reflectedHeaderFields {
		value := resp.Header.Get(key)
		if value != "" {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		context.Errorf("io.Copy after %d bytes: %s", n, err)
	}
}

func init() {
	http.HandleFunc("/", handler)
}
