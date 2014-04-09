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
	"time"
)

// The code in this file has to do communicating with the meek-http-helper
// browser extension.

type JSONRequest struct {
	Method string            `json:"method,omitempty"`
	URL    string            `json:"url,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Body   []byte            `json:"body,omitempty"`
}

type JSONResponse struct {
	Error  string `json:"error,omitempty"`
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

// Ask a locally running browser extension to make the request for us.
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
