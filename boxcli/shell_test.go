// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.
package boxcli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testShellHello(t *testing.T, name string) {
	ci, _ := strconv.ParseBool(os.Getenv("CI"))
	if _, err := exec.LookPath(name); err != nil && !ci {
		t.Skipf("Skipping because %s isn't installed or in your PATH.", name)
	}

	sh := newShell(t, name)
	sh.parentIO.write(t, "devbox init")
	sh.parentIO.write(t, "devbox add hello")
	go func() {
		for {
			sh.parentIO.read(t)
		}
	}()
	sh.startDevboxShell(t)

	sh.devboxIO.write(t, `echo "My name is: $0"`)
	out := sh.devboxIO.read(t)
	if !strings.HasSuffix(out, name) {
		t.Errorf("Shell says its name is %q, but want it to contain %q.", out, name)
	}

	sh.devboxIO.write(t, "hello")
	out = sh.devboxIO.read(t)
	want := "Hello, world!"
	if out != "Hello, world!" {
		t.Errorf("Got hello command output %q, want %q.", out, want)
	}
}

func TestShellHelloBash(t *testing.T) { testShellHello(t, "bash") }
func TestShellHelloDash(t *testing.T) { testShellHello(t, "dash") }
func TestShellHelloZsh(t *testing.T)  { testShellHello(t, "zsh") }

const (
	// shellMaxStartupReads is the maximum number of lines to read when
	// waiting for a shell prompt.
	shellMaxStartupReads = 10_000

	shellReadTimeout  = 10 * time.Second
	shellWriteTimeout = 10 * time.Second
)

// shellIO allows tests to write input and read output to and from a shell.
type shellIO struct {
	// errPrefix is an arbitrary string to include in test errors and logs.
	errPrefix string

	// inR and inW are the read and write ends of the shell's standard input
	// pipe.
	inR *os.File
	inW *os.File

	// outR and outW are the read and write ends of the shell's standard
	// output and error pipe.
	outR *os.File
	outW *os.File

	// out buffers outR for delimiting lines.
	out *bufio.Reader
}

// newShellIO creates the necessary pipes for communicating with a shell. Test
// errors and logs will include the provided prefix to help differentiate
// between multiple shells in a single test.
func newShellIO(t *testing.T, errPrefix string) shellIO {
	t.Helper()
	shio := shellIO{errPrefix: errPrefix}

	var err error
	shio.inR, shio.inW, err = os.Pipe()
	if err != nil {
		t.Fatal("Error creating shell input pipe:", err)
	}
	t.Cleanup(func() {
		shio.inR.Close()
		shio.inW.Close()
	})

	shio.outR, shio.outW, err = os.Pipe()
	if err != nil {
		t.Fatal("Error creating shell output pipe:", err)
	}
	t.Cleanup(func() {
		shio.outR.Close()
		shio.outW.Close()
	})
	shio.out = bufio.NewReader(shio.outR)
	return shio
}

// read reads a single line of output from the shell. It strips any leading or
// trailing whitespace, including the trailing newline.
func (s shellIO) read(t *testing.T) string {
	t.Helper()

	start := time.Now()
	err := s.outR.SetReadDeadline(start.Add(shellReadTimeout))
	if err != nil {
		t.Fatalf("%s/read(%s): error setting timeout: %v", s.errPrefix, time.Since(start), err)
	}
	defer func() {
		err := s.outR.SetReadDeadline(time.Time{})
		if err != nil {
			t.Fatalf("%s/read(%s): error resetting timeout: %v", s.errPrefix, time.Since(start), err)
		}
	}()

	line, err := s.out.ReadString('\n')
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("%s/read(%s): timed out after %s", s.errPrefix, time.Since(start), shellReadTimeout)
		}
		t.Fatalf("%s/read(%s): error: %v", s.errPrefix, time.Since(start), err)
	}
	line = strings.TrimSpace(line)
	t.Logf("%s/read(%s): %s", s.errPrefix, time.Since(start), line)
	return line
}

// write writes one ore more lines of input to the shell. It strips any leading
// or trailing whitespace and ensures that there is a single trailing newline
// before writing.
func (s shellIO) write(t *testing.T, line string) {
	t.Helper()

	start := time.Now()
	err := s.outR.SetWriteDeadline(start.Add(shellWriteTimeout))
	if err != nil {
		t.Fatalf("%s/write(%s): error setting timeout: %v", s.errPrefix, time.Since(start), err)
	}
	defer func() {
		err := s.outR.SetWriteDeadline(time.Time{})
		if err != nil {
			t.Fatalf("%s/write(%s): error resetting timeout: %v", s.errPrefix, time.Since(start), err)
		}
	}()

	line = strings.TrimSpace(line) + "\n"
	_, err = io.WriteString(s.inW, line)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("%s/write(%s): timed out after %s", s.errPrefix, time.Since(start), shellWriteTimeout)
		}
		t.Fatalf("%s/write(%s): error: %v", s.errPrefix, time.Since(start), err)
	}
	t.Logf("%s/write(%s): %s", s.errPrefix, time.Since(start), line)
}

// writef formats a fmt.Printf string and writes it to the shell.
func (s shellIO) writef(t *testing.T, format string, a ...any) {
	t.Helper()
	s.write(t, fmt.Sprintf(format, a...))
}

// doneWriting closes the shell's standard input, indicating that the test
// doesn't have any additional input. This will also cause the shell to exit
// after its last command terminates.
func (s shellIO) doneWriting(t *testing.T) { //nolint:unused
	t.Helper()

	err := s.inR.Close()
	if err != nil {
		t.Fatalf("Error closing input reader for %s shell: %v", s.errPrefix, err)
	}
}

// close closes the shell's input and output pipes.
func (s shellIO) close(t *testing.T) {
	t.Helper()

	if err := s.inW.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Error closing input writer for %s shell: %v", s.errPrefix, err)
	}
	if err := s.inR.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Error closing input reader for %s shell: %v", s.errPrefix, err)
	}
	if err := s.outR.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Error closing output reader for %s shell: %v", s.errPrefix, err)
	}
	if err := s.outW.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Error closing output writer for %s shell: %v", s.errPrefix, err)
	}
}

// shell controls external shell processes to aid in testing interactive devbox
// shells.
type shell struct {
	cmd      *exec.Cmd
	parentIO shellIO
	exited   bool

	devboxIO    shellIO
	devboxInFd  uintptr
	devboxOutFd uintptr
}

// newShell spawns a new shell process. It allocates 2 additional file
// descriptors for use with a devbox subshell.
func newShell(t *testing.T, name string) *shell {
	t.Helper()

	sh := shell{
		parentIO: newShellIO(t, "parent"),
		cmd:      exec.Command(name, "-s"),
	}
	sh.cmd.Dir = t.TempDir()
	sh.cmd.Stdin = sh.parentIO.inR
	sh.cmd.Stdout = sh.parentIO.outW
	sh.cmd.Stderr = sh.parentIO.outW
	sh.cmd.Env = append(os.Environ(), "SHELL="+name)

	// We need to preallocate a pipe for a devbox subshell so that parent
	// shell process has the file descriptors to pass to the devbox shell.
	//
	// The file descriptor for each file in cmd.ExtraFiles becomes its
	// index + 1. In startDevBoxShell we execute a command that redirects
	// to these descriptors.
	sh.devboxIO = newShellIO(t, "devbox")
	sh.cmd.ExtraFiles = []*os.File{sh.devboxIO.inR, sh.devboxIO.outW}
	sh.devboxInFd = 3
	sh.devboxOutFd = 4

	if err := sh.cmd.Start(); err != nil {
		t.Fatal("Error starting shell:", err)
	}
	t.Cleanup(func() {
		sh.exit(t)
	})
	return &sh
}

// startDevboxShell writes a command to the parent shell's input to start a new
// devbox subshell. It redirects the subshell's standard streams so that tests
// can communicate with the devbox shell via sh.devboxIO.
//
// After issuing the devbox shell command in the parent shell, startDevboxShell
// writes a test command to the child devbox shell and waits for its output by
// repeatedly calling read. It fails the test if a read times out, or if it
// reads more than shellMaxStartupReads lines without seeing the expected
// output.
func (sh *shell) startDevboxShell(t *testing.T) {
	t.Helper()

	sh.parentIO.writef(t, "devbox shell <&%d >&%d 2>&1", sh.devboxInFd, sh.devboxOutFd)
	echo := "Devbox started successfully!"
	sh.devboxIO.writef(t, `echo "%s"`, echo)

	i := 0
	for i = 0; i < shellMaxStartupReads; i++ {
		if strings.Contains(sh.devboxIO.read(t), echo) {
			return
		}
	}
	t.Fatalf("Didn't get a devbox shell prompt after reading %d lines.", i)
}

// exit closes the devbox and parent shell IO streams and waits for the parent
// shell to exit.
func (sh *shell) exit(t *testing.T) {
	t.Helper()

	if sh.exited {
		return
	}
	sh.devboxIO.close(t)
	sh.parentIO.close(t)
	if err := sh.cmd.Wait(); err != nil {
		t.Fatal("Error waiting for shell to exit:", err)
	}
	sh.exited = true
}
