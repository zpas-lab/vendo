package main

import (
	"bytes"
	"reflect"
	"regexp"
	"testing"
)

func Test_Command_LogModes(test *testing.T) {
	cases := []struct {
		note string
		*Cmd
		stderr *regexp.Regexp
		isErr  bool
	}{

		// DEFAULT MODE (LogOnError)

		{
			note:   "default mode, success => empty stderr",
			Cmd:    Command("go", "list", "--", "net/http"),
			stderr: regexp.MustCompile(`^$`), // empty
			isErr:  false,
		},
		{
			note: "default mode, error => cmd + output in stderr",
			Cmd:  Command("go", "list", "--", "net/http", "foobarNOTEXISTING"),
			stderr: regexp.MustCompile(`^# go list -- net/http foobarNOTEXISTING
can't load package: package foobarNOTEXISTING: cannot find package "foobarNOTEXISTING" in any of:
[\s\S]*
net/http
?$`),
			isErr: true,
		},
		{
			note: "changed env, error => cmd + env diff + output in stderr",
			Cmd: Command("go", "list", "--", "foobarNOTEXISTING").
				Setenv("FOOBAX=sOmEtHiNg STuppid", "GOPATH="),
			stderr: regexp.MustCompile(`^# FOOBAX=sOmEtHiNg STuppid GOPATH= go list -- foobarNOTEXISTING
can't load package: package foobarNOTEXISTING: cannot find package "foobarNOTEXISTING" in any of:
[\s\S]*$`),
			isErr: true,
		},

		// MODE LogAlways

		{
			note: "LogAlways, success => cmd in stderr, but no output",
			Cmd: Command("go", "list", "--", "net/http").
				LogAlways(),
			stderr: regexp.MustCompile(`^# go list -- net/http[\s]*$`),
			isErr:  false,
		},
		{
			note: "LogAlways, error => cmd + output in stderr",
			Cmd: Command("go", "list", "--", "net/http", "foobarNOTEXISTING").
				LogAlways(),
			stderr: regexp.MustCompile(`^# go list -- net/http foobarNOTEXISTING
can't load package: package foobarNOTEXISTING: cannot find package "foobarNOTEXISTING" in any of:
[\s\S]*
net/http
?$`),
			isErr: true,
		},

		// MODE LogNever

		{
			note: "LogNever, success => empty stderr",
			Cmd: Command("go", "list", "--", "net/http").
				LogNever(),
			stderr: regexp.MustCompile(`^$`),
			isErr:  false,
		},
		{
			note: "LogNever, changed env, error => empty stderr",
			Cmd: Command("go", "list", "--", "net/http", "foobarNOTEXISTING").
				LogNever().
				Setenv("FOOBAX=sOmEtHiNg STuppid", "GOPATH="),
			stderr: regexp.MustCompile(`^$`),
			isErr:  true,
		},
	}

	for _, c := range cases {
		buf := bytes.NewBuffer(nil)
		stderr = buf
		_, err := c.Cmd.OutputLines()

		if c.isErr != (err != nil) {
			test.Errorf("case %q [%s] expected isErr=%v, got err=%v",
				c.note, c.Cmd.Cmd.Args, c.isErr, err)
		}
		if !c.stderr.Match(buf.Bytes()) {
			test.Errorf("case %q [%s] expected stderr matching %q, got:\n%s",
				c.note, c.Cmd.Cmd.Args, c.stderr, buf.String())
		}
	}
}

func Test_Command_Setenv(test *testing.T) {
	cases := []struct {
		env      []string
		query    string
		expected []string
	}{
		{
			env:      []string{"GOOS=windows"},
			query:    "GOOS",
			expected: []string{"windows"},
		},
		{
			env:      []string{"GOOS=linux"},
			query:    "GOOS",
			expected: []string{"linux"},
		},
		{
			env:      []string{"GOARCH=arm", "GOOS=windows"},
			query:    "GOOS",
			expected: []string{"windows"},
		},
		{
			env:      []string{"GOARCH=arm", "GOOS=linux"},
			query:    "GOOS",
			expected: []string{"linux"},
		},
		{
			env:      []string{"GOPATH="},
			query:    "GOPATH",
			expected: nil,
		},
	}

	for _, c := range cases {
		lines, err := Command("go", "env", c.query).Setenv(c.env...).OutputLines()
		if err != nil {
			test.Errorf("case %v error: %v", err)
		}
		if !reflect.DeepEqual(lines, c.expected) {
			test.Errorf("case %v expected: %v, got: %v", c, c.expected, lines)
		}
	}
}

func Test_Command_OutputOneLine(test *testing.T) {
	// 0 lines -> error
	// 1 line -> ok
	// 2,3 lines -> error
	cases := []struct {
		note string
		*Cmd
		expected string
		isErr    bool
	}{
		{
			note:  "0 lines -> error",
			Cmd:   Command("go", "list", "-f", "", "--", "net/http"),
			isErr: true,
		},
		{
			note:     "1 line -> ok",
			Cmd:      Command("go", "list", "--", "net/http"),
			expected: "net/http",
			isErr:    false,
		},
		{
			note: "1 line with trailing empty line -> still ok, still '1 line'",
			Cmd: Command("go", "env", "GOOS", "GOPATH").
				Setenv("GOOS=linux", "GOPATH="),
			expected: "linux",
			isErr:    false,
		},
		{
			note:  "2 lines -> error",
			Cmd:   Command("go", "list", "--", "net", "net/http"),
			isErr: true,
		},
		{
			note:  "3 lines -> error",
			Cmd:   Command("go", "list", "--", "net", "net/http", "os"),
			isErr: true,
		},
	}

	for _, c := range cases {
		line, err := c.Cmd.OutputOneLine()
		if c.isErr != (err != nil) {
			test.Errorf("case %q [%s] expected isErr=%v, got err=%v",
				c.note, c.Cmd.Cmd.Args, c.isErr, err)
		}
		if c.isErr {
			continue
		}
		if c.expected != line {
			test.Errorf("case %q [%s] expected: %q, got: %q",
				c.note, c.Cmd.Cmd.Args, c.expected, line)
		}
	}
}

func Test_Command_OutputLines_Nil(test *testing.T) {
	lines, err := Command("go", "env", "GOPATH").Setenv("GOPATH=").OutputLines()
	if err != nil {
		test.Errorf("unexpected err=%v", err)
	}
	if lines != nil {
		test.Errorf("expected lines==nil, got: %q", lines)
	}
}

func Test_Command_OutputLines_Many(test *testing.T) {
	lines, err := Command("go", "list", "--", "net", "net/http", "os").OutputLines()
	if err != nil {
		test.Errorf("unexpected err=%v", err)
	}
	expected := []string{"net", "net/http", "os"}
	if !reflect.DeepEqual(lines, expected) {
		test.Errorf("OutputLines() expected:\n%q\ngot:\n%q",
			expected, lines)
	}
}
