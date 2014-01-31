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

const ptMethodName = "meek"
const sessionIdLength = 32
const maxPayloadLength = 0x10000
const initPollInterval = 100 * time.Millisecond
const maxPollInterval = 5 * time.Second
const pollIntervalMultiplier = 1.5

var ptInfo pt.ClientInfo

// When a connection handler starts, +1 is written to this channel; when it
// ends, -1 is written.
var handlerChan = make(chan int)

func roundTrip(u, host, sessionId string, buf []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", u, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	if host != "" {
		req.Host = host
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Session-Id", sessionId)
	return http.DefaultClient.Do(req)
}

func copyLoop(conn net.Conn, u, host, sessionId string) error {
	buf := make([]byte, 0x10000)
	var interval time.Duration

	conn.SetReadDeadline(time.Now().Add(initPollInterval))
	for {
		nr, readErr := conn.Read(buf)
		// log.Printf("read from local: %q", buf[:nr])

		resp, err := roundTrip(u, host, sessionId, buf[:nr])
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New(fmt.Sprintf("status code was %d, not %d", resp.StatusCode, http.StatusOK))
		}

		nw, err := io.Copy(conn, io.LimitReader(resp.Body, maxPayloadLength))
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
			interval = 0
		} else if interval < initPollInterval {
			interval = initPollInterval
		} else {
			interval = time.Duration(float64(interval) * pollIntervalMultiplier)
		}
		if interval > maxPollInterval {
			interval = maxPollInterval
		}
		// log.Printf("next poll %.6f s", interval.Seconds())
		conn.SetReadDeadline(time.Now().Add(interval))
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

	sessionId := genSessionId()

	urlArg, ok := conn.Req.Args.Get("url")
	var u *url.URL
	if ok {
		u, err = url.Parse(urlArg)
		if err != nil {
			return err
		}
	} else {
		// If no url arg, use SOCKS target.
		u = &url.URL{
			Scheme: "http",
			Host:   conn.Req.Target,
			Path:   "/",
		}
	}

	host := ""
	front, ok := conn.Req.Args.Get("front")
	if ok {
		host = u.Host
		u.Host = front
	}

	return copyLoop(conn, u.String(), host, sessionId)
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
}

func main() {
	var logFilename string

	flag.StringVar(&logFilename, "log", "", "name of log file")
	flag.Parse()

	if logFilename != "" {
		f, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("error opening log file: %s", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	var err error
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
