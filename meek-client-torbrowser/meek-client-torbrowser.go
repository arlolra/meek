// meek-client-torbrowser is an auxiliary program that helps with connecting
// meek-client to meek-http-helper running in Tor Browser.
//
// Sample usage in torrc (exact paths depend on platform):
// 	ClientTransportPlugin meek exec ./meek-client-torbrowser --log meek-client-torbrowser.log -- ./meek-client --url=https://meek-reflect.appspot.com/ --front=www.google.com --log meek-client.log
// Everything up to "--" is options for this program. Everything following it is
// a meek-client command line. The command line for running firefox is implicit
// and hardcoded in this program.
//
// This program, meek-client-torbrowser, starts a copy of firefox under the
// meek-http-helper profile, which must have configured the meek-http-helper
// extension. This program reads the stdout of firefox, looking for a special
// line with the listening port number of the extension, one that looks like
// "meek-http-helper: listen <address>". The meek-client command is then
// executed as given, except that a --helper option is added that points to the
// port number read from firefox.
//
// This program proxies stdin and stdout to and from meek-client, so it is
// actually meek-client that drives the pluggable transport negotiation with
// tor.
//
// The special --exit-on-stdin-eof is a special workaround for Windows. On
// Windows we don't get a detectable shutdown signal that allows us to kill the
// subprocesses we've started. Instead, use the --exit-on-stdin-eof option and
// run this program inside of terminateprocess-buffer. When
// terminateprocess-buffer is killed, it will close our stdin, and we can exit
// gracefully. --exit-on-stdin-eof and terminateprocess-buffer need to be used
// together.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
)

// This magic string is emitted by meek-http-helper.
var helperAddrPattern = regexp.MustCompile(`^meek-http-helper: listen (127\.0\.0\.1:\d+)$`)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [meek-client-torbrowser args] -- meek-client [meek-client args]\n", os.Args[0])
	flag.PrintDefaults()
}

// Log a call to os.Process.Kill.
func logKill(p *os.Process) error {
	log.Printf("killing PID %d", p.Pid)
	err := p.Kill()
	if err != nil {
		log.Print(err)
	}
	return err
}

// Log a call to os.Process.Signal.
func logSignal(p *os.Process, sig os.Signal) error {
	log.Printf("sending signal %s to PID %d", sig, p.Pid)
	err := p.Signal(sig)
	if err != nil {
		log.Print(err)
	}
	return err
}

// Run firefox and return its exec.Cmd and stdout pipe.
func runFirefox() (cmd *exec.Cmd, stdout io.Reader, err error) {
	var profilePath string
	// Mac OS X needs an absolute profile path.
	profilePath, err = filepath.Abs(firefoxProfilePath)
	if err != nil {
		return
	}
	cmd = exec.Command(firefoxPath, "-no-remote", "-profile", profilePath)
	cmd.Stderr = os.Stderr
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}
	log.Printf("running firefox command %q", cmd.Args)
	err = cmd.Start()
	if err != nil {
		return
	}
	log.Printf("firefox started with pid %d", cmd.Process.Pid)
	return cmd, stdout, nil
}

// Look for the magic meek-http-helper address string in the Reader, and return
// the address it contains. Start a goroutine to continue reading and discarding
// output of the Reader before returning.
func grepHelperAddr(r io.Reader) (string, error) {
	var helperAddr string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if m := helperAddrPattern.FindStringSubmatch(line); m != nil {
			helperAddr = m[1]
			break
		}
	}
	err := scanner.Err()
	if err != nil {
		return "", err
	}
	// Ran out of input before finding the pattern.
	if helperAddr == "" {
		return "", io.EOF
	}
	// Keep reading from the browser to avoid its output buffer filling.
	go io.Copy(ioutil.Discard, r)
	return helperAddr, nil
}

// Run meek-client and return its exec.Cmd.
func runMeekClient(helperAddr string, meekClientCommandLine []string) (cmd *exec.Cmd, err error) {
	meekClientPath := meekClientCommandLine[0]
	args := meekClientCommandLine[1:]
	args = append(args, []string{"--helper", helperAddr}...)
	cmd = exec.Command(meekClientPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("running meek-client command %q", cmd.Args)
	err = cmd.Start()
	if err != nil {
		return
	}
	log.Printf("meek-client started with pid %d", cmd.Process.Pid)
	return cmd, nil
}

func main() {
	var exitOnStdinEOF bool
	var logFilename string
	var err error

	flag.Usage = usage
	flag.BoolVar(&exitOnStdinEOF, "exit-on-stdin-eof", false, "exit when stdin is closed (use with terminateprocess-buffer)")
	flag.StringVar(&logFilename, "log", "", "name of log file")
	flag.Parse()

	if logFilename != "" {
		f, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start firefox.
	firefoxCmd, stdout, err := runFirefox()
	if err != nil {
		log.Print(err)
		return
	}
	defer logKill(firefoxCmd.Process)

	// Find out the helper's listening address.
	helperAddr, err := grepHelperAddr(stdout)
	if err != nil {
		log.Print(err)
		return
	}

	// Start meek-client with the helper address.
	meekClientCmd, err := runMeekClient(helperAddr, flag.Args())
	if err != nil {
		log.Print(err)
		return
	}
	defer logKill(meekClientCmd.Process)

	if exitOnStdinEOF {
		// On Windows, we don't get a SIGINT or SIGTERM, rather we are
		// killed without a chance to clean up our subprocesses. When
		// run inside terminateprocess-buffer, it is instead
		// terminateprocess-buffer that is killed, and we can detect
		// that event by that our stdin gets closed.
		// https://trac.torproject.org/projects/tor/ticket/9330
		go func() {
			io.Copy(ioutil.Discard, os.Stdin)
			log.Printf("synthesizing SIGTERM because of stdin close")
			sigChan <- syscall.SIGTERM
		}()
	}

	sig := <-sigChan
	log.Printf("sig %s", sig)
	err = logSignal(meekClientCmd.Process, sig)
	if err != nil {
		log.Print(err)
	}

	// If SIGINT, wait for a second SIGINT.
	if sig == syscall.SIGINT {
		sig := <-sigChan
		log.Printf("sig %s", sig)
		err = logSignal(meekClientCmd.Process, sig)
		if err != nil {
			log.Print(err)
		}
	}
}
