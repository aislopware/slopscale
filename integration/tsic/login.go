package tsic

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/aislopware/slopscale/integration/dockertestutil"
)

// loginTimeout bounds a web login's `tailscale up`. Some OIDC tests
// deliberately delay authentication for two minutes.
const loginTimeout = 5 * time.Minute

// loginExec runs command and copies its output into stdout and stderr as it
// arrives, returning the exit code.
type loginExec func(command []string, stdout, stderr io.Writer) (int, error)

// loginAttempt streams the first complete URL line while retaining all
// output. done closes only after the command returns; err can then be read
// repeatedly.
type loginAttempt struct {
	mu        sync.Mutex
	output    bytes.Buffer
	lineStart int
	foundURL  bool
	url       chan *url.URL
	done      chan struct{}
	err       error
}

func (l *loginAttempt) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	n, _ := l.output.Write(p)

	for !l.foundURL {
		line, _, complete := bytes.Cut(l.output.Bytes()[l.lineStart:], []byte{'\n'})
		if !complete {
			break
		}

		l.lineStart += len(line) + 1

		u, err := util.ParseLoginURLFromCLILogin(string(line))
		if err == nil {
			l.foundURL = true
			l.url <- u
		}
	}

	return n, nil
}

// outputString returns everything the command printed so far.
func (l *loginAttempt) outputString() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.output.String()
}

// startLogin runs command in the background and returns as soon as it has
// started; [loginAttempt.waitForURL] then returns the login URL once the CLI
// prints it, while the CLI keeps waiting for the login to complete.
func startLogin(command []string, exec loginExec) *loginAttempt {
	l := &loginAttempt{url: make(chan *url.URL, 1), done: make(chan struct{})}

	go func() {
		defer close(l.done)

		exitCode, err := exec(command, l, l)
		output := l.outputString()
		log.Printf("%v finished (exit %d): %s", command, exitCode, output)

		l.mu.Lock()
		defer l.mu.Unlock()

		switch {
		case err != nil:
			l.err = fmt.Errorf("tailscale up: %s: %w", output, err)
		case exitCode != 0:
			l.err = fmt.Errorf(
				"tailscale up exited %d: %s: %w",
				exitCode,
				output,
				dockertestutil.ErrDockertestCommandFailed,
			)
		case !l.foundURL:
			l.err = fmt.Errorf("tailscale up: %s: %w", output, util.ErrNoURLFound)
		}
	}()

	return l
}

// waitForURL returns the login URL the CLI printed, the command's error if
// it exited without one, or a timeout error. The CLI keeps running either
// way.
func (l *loginAttempt) waitForURL(timeout time.Duration) (*url.URL, error) {
	select {
	case u := <-l.url:
		return u, nil
	case <-l.done:
		if l.err != nil {
			return nil, l.err
		}

		return <-l.url, nil
	case <-time.After(timeout):
		return nil, dockertestutil.ErrDockertestCommandTimeout
	}
}
