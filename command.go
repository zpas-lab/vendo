package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// stderr can be changed in tests to capture output of Cmd's methods.
var stderr io.Writer = os.Stderr

// Verbose can be used to make all Cmds run with forced LogAlways.
var Verbose = false

// NewEnviron returns a clone of the original array of KEY=VALUE entries, with
// entries from the patch array merged (overriding existing KEYs).
//
// FIXME(mateuszc): test what happens for multiline values in real env
func NewEnviron(original []string, patch ...string) []string {

	// Parse the entries present in 'patch'
	mpatch := map[string]string{}
	for _, entry := range patch {
		split := strings.SplitN(entry, "=", 2)
		if len(split) != 2 {
			panic(fmt.Sprintf("unexpected environ patch entry: %q", entry))
		}
		mpatch[split[0]] = split[1]
	}

	// Copy only the entries not present in 'patch'
	result := []string{}
	for _, entry := range original {
		split := strings.SplitN(entry, "=", 2)
		if len(split) != 2 {
			panic(fmt.Sprintf("unexpected environ original entry: %q", entry))
		}
		_, found := mpatch[split[0]]
		if !found {
			result = append(result, entry)
		}
	}

	// Add entries from 'patch'
	result = append(result, patch...)
	return result
}

type LogMode int

const (
	// LogOnError is the default logging mode. If a Cmd method returns error,
	// the command-line is printed to os.Stderr, together with full command
	// output (stderr and stdout), and any changed environment variables.
	//
	// Example resulting output:
	//
	//	# GOPATH= go list -- net/http foobar
	//	can't load package: package foobar: cannot find package "foobar" in any of:
	//		/usr/local/go/src/foobar (from $GOROOT)
	//		($GOPATH not set)
	//	net/http
	LogOnError LogMode = iota
	// LogAlways always prints the command-line to os.Stderr. The command
	// output (stderr and stdout) is however only printed in case of error.
	LogAlways
	// LogNever never prints anything to os.Stderr.
	LogNever
)

type Cmd struct {
	Cmd *exec.Cmd
	LogMode
}

func Command(command string, args ...string) *Cmd {
	return &Cmd{
		Cmd:     exec.Command(command, args...),
		LogMode: LogOnError,
	}
}

// Append extends the cmd's argument list with args.
func (cmd *Cmd) Append(args ...string) *Cmd {
	cmd.Cmd.Args = append(cmd.Cmd.Args, args...)
	return cmd
}

// Setenv sets specified environment variables when running cmd. Each variable
// must be formatted as: "key=value".
func (cmd *Cmd) Setenv(variables ...string) *Cmd {
	if cmd.Cmd.Env == nil {
		cmd.Cmd.Env = os.Environ()
	}
	cmd.Cmd.Env = NewEnviron(cmd.Cmd.Env, variables...)
	return cmd
}

// LogAlways changes what is printed to os.Stderr (see const LogAlways).
func (cmd *Cmd) LogAlways() *Cmd {
	cmd.LogMode = LogAlways
	return cmd
}

// LogNever changes what is printed to os.Stderr (see const LogNever).
func (cmd *Cmd) LogNever() *Cmd {
	cmd.LogMode = LogNever
	return cmd
}

// LogOnError changes what is printed to os.Stderr (see const LogOnError).
func (cmd *Cmd) LogOnError() *Cmd {
	cmd.LogMode = LogOnError
	return cmd
}

func (cmd *Cmd) CombinedOutput() ([]byte, error) {
	if cmd.LogMode == LogAlways || Verbose {
		cmd.printCmdWithEnv()
	}
	out, err := cmd.Cmd.CombinedOutput()
	if err != nil {
		if cmd.LogMode == LogOnError {
			cmd.printCmdWithEnv()
		}
		if cmd.LogMode != LogNever || Verbose {
			stderr.Write(out)
		}
		return nil, err
	}
	return out, nil
}

// OutputLines runs the command and returns trimmed stdout+stderr output split
// into lines.
func (cmd *Cmd) OutputLines() ([]string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		// Cannot leave this case to strings.Split(), as it would give us []string{""}
		return nil, nil
	}
	lines := strings.Split(string(out), "\n")
	return lines, nil
}

// OutputOneLine runs the command, verifies that exactly one line was printed to
// stdout+stderr, and returns it.
func (cmd *Cmd) OutputOneLine() (string, error) {
	lines, err := cmd.OutputLines()
	if err != nil {
		return "", err
	}
	if len(lines) != 1 {
		if cmd.LogMode == LogOnError {
			cmd.printCmdWithEnv()
		}
		if cmd.LogMode != LogNever {
			fmt.Fprintln(stderr, strings.Join(lines, "\n"))
		}
		return "", fmt.Errorf("expected one line of output from %s, got %d", cmd.Cmd.Args[0], len(lines))
	}
	return lines[0], nil
}

func (cmd *Cmd) DiscardOutput() error {
	_, err := cmd.OutputLines()
	return err
}

func envToMap(env []string) map[string]string {
	m := map[string]string{}
	for _, entry := range env {
		key := strings.SplitN(entry, "=", 2)[0]
		m[key] = entry
		// TODO(mateuszc): if entries repeat, we override with later. Is that ok?
	}
	return m
}

func (cmd *Cmd) printCmdWithEnv() {
	// Detect tweaks of environment variables.
	// Note: this won't show changes done using os.Setenv()
	diff := []string{}
	if cmd.Cmd.Env != nil {
		original := envToMap(os.Environ())
		changed := envToMap(cmd.Cmd.Env)
		for k, v := range changed {
			if original[k] != v {
				diff = append(diff, v+" ")
			}
		}
		for k := range original {
			_, found := changed[k]
			if !found {
				diff = append(diff, k+"= ")
			}
		}
	}
	sort.Strings(diff)

	// Example output:
	//	# GOOS=windows GOARCH=amd64 go build .
	fmt.Fprintf(stderr, "# %s%s\n",
		strings.Join(diff, ""),
		strings.Join(cmd.Cmd.Args, " "))
}
