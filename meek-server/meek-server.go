package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"
)

import "git.torproject.org/pluggable-transports/goptlib.git"

const ptMethodName = "meek"
const minSessionIdLength = 32
const maxPayloadLength = 0x10000

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

type State struct {
	sessionMap map[string]*net.TCPConn
}

func NewState() *State {
	state := new(State)
	state.sessionMap = make(map[string]*net.TCPConn)
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

func (state *State) Post(w http.ResponseWriter, req *http.Request) {
	var err error

	id := req.Header.Get("x-session-id")
	if len(id) < minSessionIdLength {
		httpBadRequest(w)
		return
	}

	or, ok := state.sessionMap[id]
	if ok {
		log.Printf("already existing session id %q", id)
	} else {
		log.Printf("unknown session id %q; creating new session", id)
		or, err = pt.DialOr(&ptInfo, req.RemoteAddr, ptMethodName)
		if err != nil {
			log.Printf("error in DialOr: %s", err)
			httpInternalServerError(w)
			return
		}
		state.sessionMap[id] = or
	}

	body := http.MaxBytesReader(w, req.Body, maxPayloadLength)
	_, err = io.Copy(or, body)
	if err != nil {
		log.Printf("error copying body to ORPort: %s", err)
		return
	}

	buf := make([]byte, maxPayloadLength)
	or.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	n, err := or.Read(buf)
	if err != nil {
		if e, ok := err.(net.Error); !ok || !e.Timeout() {
			log.Printf("error reading from ORPort: %s", err)
			return
		}
	}
	// log.Printf("read %d bytes from ORPort: %q", n, buf[:n])
	n, err = w.Write(buf[:n])
	if err != nil {
		log.Printf("error writing to response: %s", err)
		return
	}
	// log.Printf("wrote %d bytes to response", n)
}

func startListener(network string, addr *net.TCPAddr) (net.Listener, error) {
	ln, err := net.ListenTCP(network, addr)
	if err != nil {
		return nil, err
	}
	state := NewState()
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
