// Usage:
//   meek-client-torbrowser --log meek-client-torbrowser.log -- meek-client --url=https://meek-reflect.appspot.com/ --front=www.google.com --log meek-client.log
//
// The meek-client-torbrowser program starts a copy of Tor Browser running
// meek-http-helper in a special profile, and then starts meek-client set up to
// use the browser helper.
//
// Arguments to this program are passed unmodified to meek-client, with the
// addition of a --helper option pointing to the browser helper.
package main

import (
	"bufio"
	"flag"
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

var helperAddrPattern *regexp.Regexp

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
	var logFilename string
	var err error

	flag.StringVar(&logFilename, "log", "", "name of log file")
	flag.Parse()

	if logFilename != "" {
		f, err := os.OpenFile("meek-client-torbrowser.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// This magic string is emitted by meek-http-helper.
	helperAddrPattern, err = regexp.Compile(`^meek-http-helper: listen (127\.0\.0\.1:\d+)$`)
	if err != nil {
		log.Fatal(err)
	}

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
		// On Windows, we don't get a SIGINT or SIGTERM, rather we are killed
		// without a chance to clean up our subprocesses. When run inside
		// terminateprocess-buffer, it is instead terminateprocess-buffer that
		// is killed, and we can detect that event by that our stdin gets
		// closed.
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
