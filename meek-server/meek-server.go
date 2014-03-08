package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"time"
)

import "git.torproject.org/pluggable-transports/goptlib.git"

const ptMethodName = "meek"
const minSessionIdLength = 32
const maxPayloadLength = 0x10000
const turnaroundDeadline = 10 * time.Millisecond
const maxSessionStaleness = 120 * time.Second

var ptInfo pt.ServerInfo

// When a connection handler starts, +1 is written to this channel; when it
// ends, -1 is written.
var handlerChan = make(chan int)

func httpBadRequest(w http.ResponseWriter) {
	http.Error(w, "Bad request.\n", http.StatusBadRequest)
}

func httpInternalServerError(w http.ResponseWriter) {
	http.Error(w, "Internal server error.\n", http.StatusInternalServerError)
}

type Session struct {
	Or       *net.TCPConn
	LastSeen time.Time
}

func (session *Session) Touch() {
	session.LastSeen = time.Now()
}

func (session *Session) Expired() bool {
	return time.Since(session.LastSeen) > maxSessionStaleness
}

type State struct {
	sessionMap map[string]*Session
	lock       sync.Mutex
}

func NewState() *State {
	state := new(State)
	state.sessionMap = make(map[string]*Session)
	return state
}

func (state *State) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	handlerChan <- 1
	defer func() {
		handlerChan <- -1
	}()

	switch req.Method {
	case "GET":
		state.Get(w, req)
	case "POST":
		state.Post(w, req)
	default:
		httpBadRequest(w)
	}
}

func (state *State) Get(w http.ResponseWriter, req *http.Request) {
	if path.Clean(req.URL.Path) != "/" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("I’m just a happy little web server.\n"))
}

func (state *State) GetSession(sessionId string, req *http.Request) (*Session, error) {
	state.lock.Lock()
	defer state.lock.Unlock()

	session := state.sessionMap[sessionId]
	if session == nil {
		// log.Printf("unknown session id %q; creating new session", sessionId)

		or, err := pt.DialOr(&ptInfo, req.RemoteAddr, ptMethodName)
		if err != nil {
			return nil, err
		}
		session = &Session{Or: or}
		state.sessionMap[sessionId] = session
	}
	session.Touch()

	return session, nil
}

func transact(session *Session, w http.ResponseWriter, req *http.Request) error {
	body := http.MaxBytesReader(w, req.Body, maxPayloadLength+1)
	_, err := io.Copy(session.Or, body)
	if err != nil {
		return errors.New(fmt.Sprintf("copying body to ORPort: %s", err))
	}

	buf := make([]byte, maxPayloadLength)
	session.Or.SetReadDeadline(time.Now().Add(turnaroundDeadline))
	n, err := session.Or.Read(buf)
	if err != nil {
		if e, ok := err.(net.Error); !ok || !e.Timeout() {
			httpInternalServerError(w)
			return errors.New(fmt.Sprintf("reading from ORPort: %s", err))
		}
	}
	// log.Printf("read %d bytes from ORPort: %q", n, buf[:n])
	n, err = w.Write(buf[:n])
	if err != nil {
		return errors.New(fmt.Sprintf("writing to response: %s", err))
	}
	// log.Printf("wrote %d bytes to response", n)
	return nil
}

func (state *State) Post(w http.ResponseWriter, req *http.Request) {
	sessionId := req.Header.Get("X-Session-Id")
	if len(sessionId) < minSessionIdLength {
		httpBadRequest(w)
		return
	}

	session, err := state.GetSession(sessionId, req)
	if err != nil {
		log.Print(err)
		httpInternalServerError(w)
		return
	}

	err = transact(session, w, req)
	if err != nil {
		log.Print(err)
		state.CloseSession(sessionId)
		return
	}
}

func (state *State) CloseSession(sessionId string) {
	state.lock.Lock()
	defer state.lock.Unlock()
	// log.Printf("closing session %q", sessionId)
	session, ok := state.sessionMap[sessionId]
	if ok {
		session.Or.Close()
		delete(state.sessionMap, sessionId)
	}
}

func (state *State) ExpireSessions() {
	for {
		time.Sleep(maxSessionStaleness / 2)
		state.lock.Lock()
		for sessionId, session := range state.sessionMap {
			if session.Expired() {
				// log.Printf("deleting expired session %q", sessionId)
				session.Or.Close()
				delete(state.sessionMap, sessionId)
			}
		}
		state.lock.Unlock()
	}
}

func startListener(network string, addr *net.TCPAddr) (net.Listener, error) {
	ln, err := net.ListenTCP(network, addr)
	if err != nil {
		return nil, err
	}
	state := NewState()
	go state.ExpireSessions()
	server := &http.Server{
		Handler: state,
	}
	go func() {
		defer ln.Close()
		err = server.Serve(ln)
		if err != nil {
			log.Printf("Error in Serve: %s", err)
		}
	}()
	return ln, nil
}

func main() {
	var logFilename string
	var port int

	flag.StringVar(&logFilename, "log", "", "name of log file")
	flag.IntVar(&port, "port", 0, "port to listen on")
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
	ptInfo, err = pt.ServerSetup([]string{ptMethodName})
	if err != nil {
		log.Fatalf("error in ServerSetup: %s", err)
	}

	log.Printf("starting")
	listeners := make([]net.Listener, 0)
	for _, bindaddr := range ptInfo.Bindaddrs {
		if port != 0 {
			bindaddr.Addr.Port = port
		}
		switch bindaddr.MethodName {
		case ptMethodName:
			ln, err := startListener("tcp", bindaddr.Addr)
			if err != nil {
				pt.SmethodError(bindaddr.MethodName, err.Error())
				break
			}
			pt.Smethod(bindaddr.MethodName, ln.Addr())
			log.Printf("listening on %s", ln.Addr())
			listeners = append(listeners, ln)
		default:
			pt.SmethodError(bindaddr.MethodName, "no such method")
		}
	}
	pt.SmethodsDone()

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
