// meek-client is the client transport plugin for the meek pluggable transport.
//
// Sample usage in torrc:
// 	Bridge meek 0.0.2.0:1
// 	ClientTransportPlugin meek exec ./meek-client --url=https://meek-reflect.appspot.com/ --front=www.google.com --log meek-client.log
// The transport ignores the bridge address 0.0.2.0:1 and instead connects to
// the URL given by --url. When --front is given, the domain in the URL is
// replaced by the front domain for the purpose of the DNS lookup, TCP
// connection, and TLS SNI, but the HTTP Host header in the request will be the
// one in --url. (For example, in the configuration above, the connection will
// appear on the outside to be going to www.google.com, but it will actually be
// dispatched to meek-reflect.appspot.com by the Google frontend server.)
//
// Most user configuration can happen either through SOCKS args (i.e., args on a
// Bridge line) or through command line options. SOCKS args take precedence
// per-connection over command line options. For example, this configuration
// using SOCKS args:
// 	Bridge meek 0.0.2.0:1 url=https://meek-reflect.appspot.com/ front=www.google.com
// 	ClientTransportPlugin meek exec ./meek-client
// is the same as this one using command line options:
// 	Bridge meek 0.0.2.0:1
// 	ClientTransportPlugin meek exec ./meek-client --url=https://meek-reflect.appspot.com/ --front=www.google.com
// The advantage of SOCKS args is that multiple Bridge lines can have different
// configurations, but it requires a newer tor.
//
// The --helper option prevents this program from doing any network operations
// itself. Rather, it will send all requests through a browser extension that
// makes HTTP requests.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

import "git.torproject.org/pluggable-transports/goptlib.git"

const (
	ptMethodName = "meek"
	// A session ID is a randomly generated string that identifies a
	// long-lived session. We split a TCP stream across multiple HTTP
	// requests, and those with the same session ID belong to the same
	// stream.
	sessionIdLength = 32
	// The size of the largest chunk of data we will read from the SOCKS
	// port before forwarding it in a request, and the maximum size of a
	// body we are willing to handle in a reply.
	maxPayloadLength = 0x10000
	// We must poll the server to see if it has anything to send; there is
	// no way for the server to push data back to us until we send an HTTP
	// request. When a timer expires, we send a request even if it has an
	// empty body. The interval starts at this value and then grows.
	initPollInterval = 100 * time.Millisecond
	// Maximum polling interval.
	maxPollInterval = 5 * time.Second
	// Geometric increase in the polling interval each time we fail to read
	// data.
	pollIntervalMultiplier = 1.5
	// Try an HTTP roundtrip at most this many times.
	maxTries = 10
	// Wait this long between retries.
	retryDelay = 30 * time.Second
	// Safety limits on interaction with the HTTP helper.
	maxHelperResponseLength = 10000000
	helperReadTimeout       = 60 * time.Second
	helperWriteTimeout      = 2 * time.Second
)

var ptInfo pt.ClientInfo

// Store for command line options.
var options struct {
	URL        string
	Front      string
	ProxyURL   *url.URL
	HelperAddr *net.TCPAddr
}

// When a connection handler starts, +1 is written to this channel; when it
// ends, -1 is written.
var handlerChan = make(chan int)

// RequestInfo encapsulates all the configuration used for a request–response
// roundtrip, including variables that may come from SOCKS args or from the
// command line.
type RequestInfo struct {
	// What to put in the X-Session-ID header.
	SessionID string
	// The URL to request.
	URL *url.URL
	// The Host header to put in the HTTP request (optional and may be
	// different from the host name in URL).
	Host string
	// URL of an upstream proxy to use. If nil, no proxy is used.
	ProxyURL *url.URL
}

// Do an HTTP roundtrip using the payload data in buf and the request metadata
// in info.
func roundTripWithHTTP(buf []byte, info *RequestInfo) (*http.Response, error) {
	tr := new(http.Transport)
	if info.ProxyURL != nil {
		if info.ProxyURL.Scheme != "http" {
			panic(fmt.Sprintf("don't know how to use proxy %s", info.ProxyURL.String()))
		}
		tr.Proxy = http.ProxyURL(info.ProxyURL)
	}
	req, err := http.NewRequest("POST", info.URL.String(), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	if info.Host != "" {
		req.Host = info.Host
	}
	req.Header.Set("X-Session-Id", info.SessionID)
	return tr.RoundTrip(req)
}

// Do a roundtrip, trying at most limit times if there is an HTTP status other
// than 200. In case all tries result in error, returns the last error seen.
//
// Retrying the request immediately is a bit bogus, because we don't know if the
// remote server received our bytes or not, so we may be sending duplicates,
// which will cause the connection to die. The alternative, though, is to just
// kill the connection immediately. A better solution would be a system of
// acknowledgements so we know what to resend after an error.
func roundTripRetries(buf []byte, info *RequestInfo, limit int) (*http.Response, error) {
	roundTrip := roundTripWithHTTP
	if options.HelperAddr != nil {
		roundTrip = roundTripWithHelper
	}
	var resp *http.Response
	var err error
again:
	limit--
	resp, err = roundTrip(buf, info)
	// Retry only if the HTTP roundtrip completed without error, but
	// returned a status other than 200. Other kinds of errors and success
	// with 200 always return immediately.
	if err == nil && resp.StatusCode != http.StatusOK {
		err = errors.New(fmt.Sprintf("status code was %d, not %d", resp.StatusCode, http.StatusOK))
		if limit > 0 {
			log.Printf("%s; trying again after %.f seconds (%d)", err, retryDelay.Seconds(), limit)
			time.Sleep(retryDelay)
			goto again
		}
	}
	return resp, err
}

// Send the data in buf to the remote URL, wait for a reply, and feed the reply
// body back into conn.
func sendRecv(buf []byte, conn net.Conn, info *RequestInfo) (int64, error) {
	resp, err := roundTripRetries(buf, info, maxTries)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return io.Copy(conn, io.LimitReader(resp.Body, maxPayloadLength))
}

// Repeatedly read from conn, issue HTTP requests, and write the responses back
// to conn.
func copyLoop(conn net.Conn, info *RequestInfo) error {
	var interval time.Duration

	ch := make(chan []byte)

	// Read from the Conn and send byte slices on the channel.
	go func() {
		var buf [maxPayloadLength]byte
		r := bufio.NewReader(conn)
		for {
			n, err := r.Read(buf[:])
			b := make([]byte, n)
			copy(b, buf[:n])
			// log.Printf("read from local: %q", b)
			ch <- b
			if err != nil {
				log.Printf("error reading from local: %s", err)
				break
			}
		}
		close(ch)
	}()

	interval = initPollInterval
loop:
	for {
		var buf []byte
		var ok bool

		// log.Printf("waiting up to %.2f s", interval.Seconds())
		// start := time.Now()
		select {
		case buf, ok = <-ch:
			if !ok {
				break loop
			}
			// log.Printf("read %d bytes from local after %.2f s", len(buf), time.Since(start).Seconds())
		case <-time.After(interval):
			// log.Printf("read nothing from local after %.2f s", time.Since(start).Seconds())
			buf = nil
		}

		nw, err := sendRecv(buf, conn, info)
		if err != nil {
			return err
		}
		/*
			if nw > 0 {
				log.Printf("got %d bytes from remote", nw)
			} else {
				log.Printf("got nothing from remote")
			}
		*/

		if nw > 0 || len(buf) > 0 {
			// If we sent or received anything, poll again
			// immediately.
			interval = 0
		} else if interval == 0 {
			// The first time we don't send or receive anything,
			// wait a while.
			interval = initPollInterval
		} else {
			// After that, wait a little longer.
			interval = time.Duration(float64(interval) * pollIntervalMultiplier)
		}
		if interval > maxPollInterval {
			interval = maxPollInterval
		}
	}

	return nil
}

func genSessionId() string {
	buf := make([]byte, sessionIdLength)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err.Error())
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// Callback for new SOCKS requests.
func handler(conn *pt.SocksConn) error {
	handlerChan <- 1
	defer func() {
		handlerChan <- -1
	}()

	defer conn.Close()
	err := conn.Grant(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	if err != nil {
		return err
	}

	var info RequestInfo
	info.SessionID = genSessionId()

	// First check url= SOCKS arg, then --url option, then SOCKS target.
	urlArg, ok := conn.Req.Args.Get("url")
	if ok {
	} else if options.URL != "" {
		urlArg = options.URL
	} else {
		urlArg = (&url.URL{
			Scheme: "http",
			Host:   conn.Req.Target,
			Path:   "/",
		}).String()
	}
	info.URL, err = url.Parse(urlArg)
	if err != nil {
		return err
	}

	// First check front= SOCKS arg, then --front option.
	front, ok := conn.Req.Args.Get("front")
	if ok {
	} else if options.Front != "" {
		front = options.Front
		ok = true
	}
	if ok {
		info.Host = info.URL.Host
		info.URL.Host = front
	}

	// First check proxy= SOCKS arg, then --proxy option/managed
	// configuration.
	proxy, ok := conn.Req.Args.Get("proxy")
	if ok {
		info.ProxyURL, err = url.Parse(proxy)
		if err != nil {
			return err
		}
	} else if options.ProxyURL != nil {
		info.ProxyURL = options.ProxyURL
	}

	return copyLoop(conn, &info)
}

func acceptLoop(ln *pt.SocksListener) error {
	defer ln.Close()
	for {
		conn, err := ln.AcceptSocks()
		if err != nil {
			log.Printf("error in AcceptSocks: %s", err)
			if e, ok := err.(net.Error); ok && !e.Temporary() {
				return err
			}
			continue
		}
		go func() {
			err := handler(conn)
			if err != nil {
				log.Printf("error in handling request: %s", err)
			}
		}()
	}
	return nil
}

// Return an error if this proxy URL doesn't work with the rest of the
// configuration.
func checkProxyURL(u *url.URL) error {
	if options.HelperAddr == nil {
		// Without the helper we only support HTTP proxies.
		if options.ProxyURL.Scheme != "http" {
			return errors.New(fmt.Sprintf("don't understand proxy URL scheme %q", options.ProxyURL.Scheme))
		}
	} else {
		// With the helper we can use HTTP and SOCKS (because it is the
		// browser that does the proxying, not us).
		// For the HTTP proxy with the Firefox helper: versions of
		// Firefox before 32, and Tor Browser before 3.6.2, leak the
		// covert Host header in HTTP proxy CONNECT requests. Using an
		// HTTP proxy cannot provide effective obfuscation without such
		// a patched Firefox.
		// https://trac.torproject.org/projects/tor/ticket/12146
		// https://gitweb.torproject.org/tor-browser.git/commitdiff/e08b91c78d919f66dd5161561ca1ad7bcec9a563
		// https://bugzilla.mozilla.org/show_bug.cgi?id=1017769
		// https://hg.mozilla.org/mozilla-central/rev/a1f6458800d4
		switch options.ProxyURL.Scheme {
		case "http", "socks5", "socks4a":
		default:
			return errors.New(fmt.Sprintf("don't understand proxy URL scheme %q", options.ProxyURL.Scheme))
		}
		if options.ProxyURL.User != nil {
			return errors.New("a proxy URL with a username or password can't be used with --helper")
		}
	}
	return nil
}

func main() {
	var helperAddr string
	var logFilename string
	var proxy string
	var err error

	flag.StringVar(&options.Front, "front", "", "front domain name if no front= SOCKS arg")
	flag.StringVar(&helperAddr, "helper", "", "address of HTTP helper (browser extension)")
	flag.StringVar(&logFilename, "log", "", "name of log file")
	flag.StringVar(&proxy, "proxy", "", "proxy URL if no proxy= SOCKS arg")
	flag.StringVar(&options.URL, "url", "", "URL to request if no url= SOCKS arg")
	flag.Parse()

	if logFilename != "" {
		f, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("error opening log file: %s", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	if helperAddr != "" {
		options.HelperAddr, err = net.ResolveTCPAddr("tcp", helperAddr)
		if err != nil {
			log.Fatalf("can't resolve helper address: %s", err)
		}
		log.Printf("using helper on %s", options.HelperAddr)
	}

	if proxy != "" {
		options.ProxyURL, err = url.Parse(proxy)
		if err != nil {
			log.Fatalf("can't parse proxy URL: %s", err)
		}
	}

	ptInfo, err = pt.ClientSetup([]string{ptMethodName})
	if err != nil {
		log.Fatalf("error in ClientSetup: %s", err)
	}
	ptProxyURL, err := PtGetProxyURL()
	if err != nil {
		PtProxyError(err.Error())
		log.Fatalf("can't get managed proxy configuration: %s", err)
	}

	// Command-line proxy overrides managed configuration.
	if options.ProxyURL == nil {
		options.ProxyURL = ptProxyURL
	}
	// Check whether we support this kind of proxy.
	if options.ProxyURL != nil {
		err = checkProxyURL(options.ProxyURL)
		if err != nil {
			PtProxyError(err.Error())
			log.Fatal(fmt.Sprintf("proxy error: %s", err))
		}
		log.Printf("using proxy %s", options.ProxyURL.String())
		if ptProxyURL != nil {
			PtProxyDone()
		}
	}

	listeners := make([]net.Listener, 0)
	for _, methodName := range ptInfo.MethodNames {
		switch methodName {
		case ptMethodName:
			ln, err := pt.ListenSocks("tcp", "127.0.0.1:0")
			if err != nil {
				pt.CmethodError(methodName, err.Error())
				break
			}
			go acceptLoop(ln)
			pt.Cmethod(methodName, ln.Version(), ln.Addr())
			log.Printf("listening on %s", ln.Addr())
			listeners = append(listeners, ln)
		default:
			pt.CmethodError(methodName, "no such method")
		}
	}
	pt.CmethodsDone()

	var numHandlers int = 0
	var sig os.Signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for first signal.
	sig = nil
	for sig == nil {
		select {
		case n := <-handlerChan:
			numHandlers += n
		case sig = <-sigChan:
			log.Printf("got signal %s", sig)
		}
	}
	for _, ln := range listeners {
		ln.Close()
	}

	if sig == syscall.SIGTERM {
		log.Printf("done")
		return
	}

	// Wait for second signal or no more handlers.
	sig = nil
	for sig == nil && numHandlers != 0 {
		select {
		case n := <-handlerChan:
			numHandlers += n
		case sig = <-sigChan:
			log.Printf("got second signal %s", sig)
		}
	}

	log.Printf("done")
}
