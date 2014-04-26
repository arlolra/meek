package main

import (
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
	sessionIdLength = 32
	maxPayloadLength = 0x10000
	initPollInterval = 100 * time.Millisecond
	maxPollInterval = 5 * time.Second
	pollIntervalMultiplier = 1.5
	maxHelperResponseLength = 10000000
	helperReadTimeout = 60 * time.Second
	helperWriteTimeout = 2 * time.Second
)

var ptInfo pt.ClientInfo

var options struct {
	URL          string
	Front        string
	HTTPProxyURL *url.URL
	HelperAddr   *net.TCPAddr
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
	// URL of an HTTP proxy to use. If nil, the default net/http library's
	// behavior is used, which is to check the HTTP_PROXY and http_proxy
	// environment for a proxy URL.
	HTTPProxyURL *url.URL
}

func roundTripWithHTTP(buf []byte, info *RequestInfo) (*http.Response, error) {
	tr := http.DefaultTransport
	if info.HTTPProxyURL != nil {
		tr = &http.Transport{
			Proxy: http.ProxyURL(info.HTTPProxyURL),
		}
	}
	req, err := http.NewRequest("POST", info.URL.String(), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	if info.Host != "" {
		req.Host = info.Host
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Session-Id", info.SessionID)
	return tr.RoundTrip(req)
}

func sendRecv(buf []byte, conn net.Conn, info *RequestInfo) (int64, error) {
	roundTrip := roundTripWithHTTP
	if options.HelperAddr != nil {
		roundTrip = roundTripWithHelper
	}
	resp, err := roundTrip(buf, info)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, errors.New(fmt.Sprintf("status code was %d, not %d", resp.StatusCode, http.StatusOK))
	}

	return io.Copy(conn, io.LimitReader(resp.Body, maxPayloadLength))
}

func copyLoop(conn net.Conn, info *RequestInfo) error {
	buf := make([]byte, maxPayloadLength)
	var interval time.Duration

	interval = initPollInterval
	for {
		conn.SetReadDeadline(time.Now().Add(interval))
		// log.Printf("next poll %.6f s", interval.Seconds())
		nr, readErr := conn.Read(buf)
		// log.Printf("read from local: %q", buf[:nr])

		nw, err := sendRecv(buf[:nr], conn, info)
		if err != nil {
			return err
		}
		// log.Printf("read from remote: %d", nw)

		if readErr != nil {
			if e, ok := readErr.(net.Error); !ok || !e.Timeout() {
				return readErr
			}
		}

		if nw > 0 {
			interval = initPollInterval
		} else {
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

	// First check http-proxy= SOCKS arg, then --http-proxy option.
	httpProxy, ok := conn.Req.Args.Get("http-proxy")
	if ok {
		info.HTTPProxyURL, err = url.Parse(httpProxy)
		if err != nil {
			return err
		}
	} else if options.HTTPProxyURL != nil {
		info.HTTPProxyURL = options.HTTPProxyURL
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

func main() {
	var helperAddr string
	var httpProxy string
	var logFilename string
	var err error

	flag.StringVar(&options.Front, "front", "", "front domain name if no front= SOCKS arg")
	flag.StringVar(&helperAddr, "helper", "", "address of HTTP helper (browser extension)")
	flag.StringVar(&httpProxy, "http-proxy", "", "HTTP proxy URL (default from HTTP_PROXY environment variable)")
	flag.StringVar(&logFilename, "log", "", "name of log file")
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

	if helperAddr != "" && httpProxy != "" {
		log.Fatalf("--helper and --http-proxy can't be used together")
	}

	if helperAddr != "" {
		options.HelperAddr, err = net.ResolveTCPAddr("tcp", helperAddr)
		if err != nil {
			log.Fatalf("can't resolve helper address: %s", err)
		}
		log.Printf("using helper on %s", options.HelperAddr)
	}

	if httpProxy != "" {
		options.HTTPProxyURL, err = url.Parse(httpProxy)
		if err != nil {
			log.Fatalf("can't parse HTTP proxy URL: %s", err)
		}
	}

	ptInfo, err = pt.ClientSetup([]string{ptMethodName})
	if err != nil {
		log.Fatalf("error in ClientSetup: %s", err)
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

	// wait for first signal
	sig = nil
	for sig == nil {
		select {
		case n := <-handlerChan:
			numHandlers += n
		case sig = <-sigChan:
		}
	}
	for _, ln := range listeners {
		ln.Close()
	}

	if sig == syscall.SIGTERM {
		return
	}

	// wait for second signal or no more handlers
	sig = nil
	for sig == nil && numHandlers != 0 {
		select {
		case n := <-handlerChan:
			numHandlers += n
		case sig = <-sigChan:
		}
	}

	log.Printf("done")
}
