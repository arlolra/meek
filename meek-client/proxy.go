package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
)

import "git.torproject.org/pluggable-transports/goptlib.git"

// The code in this file has to do with configuring an upstream proxy, whether
// through the command line or the managed interface of proposal 232
// (TOR_PT_PROXY).
//
// https://gitweb.torproject.org/torspec.git/blob/HEAD:/proposals/232-pluggable-transports-through-proxy.txt

// Get the upstream proxy URL. Returns nil if no proxy is requested. The
// function ensures that the Scheme and Host fields are set; i.e., that the URL
// is absolute. This function reads the environment variable TOR_PT_PROXY.
//
// This function doesn't check that the scheme is one of Tor's supported proxy
// schemes; that is, one of "http", "socks5", or "socks4a". The caller must be
// able to handle any returned scheme (which may be by calling PtProxyError if
// it doesn't know how to handle the scheme).
func PtGetProxyURL() (*url.URL, error) {
	rawurl := os.Getenv("TOR_PT_PROXY")
	if rawurl == "" {
		return nil, nil
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		return nil, errors.New("missing scheme")
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	return u, nil
}

// Emit a PROXY-ERROR line with explanation text.
func PtProxyError(msg string) {
	fmt.Fprintf(pt.Stdout, "PROXY-ERROR %s\n", msg)
}

// Emit a PROXY DONE line. Call this after parsing the return value of
// PtGetProxyURL.
func PtProxyDone() {
	fmt.Fprintf(pt.Stdout, "PROXY DONE\n")
}
