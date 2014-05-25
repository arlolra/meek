package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The code in this file has to do communicating with the meek-http-helper
// browser extension.

type JSONRequest struct {
	Method string            `json:"method,omitempty"`
	URL    string            `json:"url,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Body   []byte            `json:"body,omitempty"`
	Proxy  *ProxySpec        `json:"proxy,omitempty"`
}

type JSONResponse struct {
	Error  string `json:"error,omitempty"`
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

// ProxySpec encodes information we need to connect through a proxy.
type ProxySpec struct {
	// Acceptable values for Type are as in proposal 232: "http", "socks5",
	// or "socks4a".
	Type string `json:"type"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Return a ProxySpec suitable for the proxy URL in u.
func makeProxySpec(u *url.URL) (*ProxySpec, error) {
	spec := new(ProxySpec)
	var err error
	var portStr string
	var port uint64

	if u == nil {
		// No proxy.
		return nil, nil
	}

	// Firefox's nsIProxyInfo doesn't allow credentials.
	if u.User != nil {
		return nil, errors.New("proxy URLs with a username or password can't be used with the helper")
	}

	switch u.Scheme {
	case "http", "socks5", "socks4a":
		spec.Type = u.Scheme
	default:
		return nil, errors.New("unknown scheme")
	}

	spec.Host, portStr, err = net.SplitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	if spec.Host == "" {
		return nil, errors.New("missing host")
	}
	port, err = strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, err
	}
	spec.Port = int(port)

	return spec, nil
}

// Do an HTTP roundtrip through the configured browser extension, using the
// payload data in buf and the request metadata in info.
func roundTripWithHelper(buf []byte, info *RequestInfo) (*http.Response, error) {
	s, err := net.DialTCP("tcp", nil, options.HelperAddr)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	// Encode our JSON.
	req := JSONRequest{
		Method: "POST",
		URL:    info.URL.String(),
		Header: make(map[string]string),
		Body:   buf,
	}
	req.Header["X-Session-Id"] = info.SessionID
	if info.Host != "" {
		req.Header["Host"] = info.Host
	}
	req.Proxy, err = makeProxySpec(options.ProxyURL)
	if err != nil {
		return nil, err
	}
	encReq, err := json.Marshal(&req)
	if err != nil {
		return nil, err
	}
	// log.Printf("encoded %s", encReq)

	// Send the request.
	s.SetWriteDeadline(time.Now().Add(helperWriteTimeout))
	err = binary.Write(s, binary.BigEndian, uint32(len(encReq)))
	if err != nil {
		return nil, err
	}
	_, err = s.Write(encReq)
	if err != nil {
		return nil, err
	}

	// Read the response.
	var length uint32
	s.SetReadDeadline(time.Now().Add(helperReadTimeout))
	err = binary.Read(s, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}
	if length > maxHelperResponseLength {
		return nil, errors.New(fmt.Sprintf("helper's returned data is too big (%d > %d)",
			length, maxHelperResponseLength))
	}
	encResp := make([]byte, length)
	_, err = io.ReadFull(s, encResp)
	if err != nil {
		return nil, err
	}
	// log.Printf("received %s", encResp)

	// Decode their JSON.
	var jsonResp JSONResponse
	err = json.Unmarshal(encResp, &jsonResp)
	if err != nil {
		return nil, err
	}
	if jsonResp.Error != "" {
		return nil, errors.New(fmt.Sprintf("helper returned error: %s", jsonResp.Error))
	}

	// Mock up an HTTP response.
	resp := http.Response{
		Status:        http.StatusText(jsonResp.Status),
		StatusCode:    jsonResp.Status,
		Body:          ioutil.NopCloser(bytes.NewReader(jsonResp.Body)),
		ContentLength: int64(len(jsonResp.Body)),
	}
	return &resp, nil
}
