// The terminateprocess-buffer program is designed to act as a
// TerminateProcess-absorbing buffer between tor and a transport plugin on
// Windows. On Windows, transport plugins are killed with a TerminateProcess,
// which doesn't give them a chance to clean up before exiting.
// https://trac.torproject.org/projects/tor/ticket/9330
// The idea of this program is that the transport plugin can read from its
// standard input, which will be closed when this program is terminated. The
// transport plugin can then treat the stdin-closed event like a SIGTERM.
package main

import (
	"io"
	"log"
	"os"
	"os/exec"
)

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		log.Fatalf("%s needs a command to run", os.Args[0])
	}
	cmd := exec.Command(args[0], args[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	io.Copy(stdin, os.Stdin)
	err = cmd.Wait()
	if err != nil {
		log.Fatal(err)
	}
}
