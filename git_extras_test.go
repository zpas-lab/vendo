package main

import "testing"

func Test_git_parseFilename(test *testing.T) {
	cases := []struct{ input, expFilename, expRest, expError string }{
		{`foo`, "foo", "", ""},
		{`"foo"`, "foo", "", ""},
		{`foo bar`, "foo", " bar", ""},
		{`"foo bar" baz`, "foo bar", " baz", ""},
		{`"b\305\272dzi\304\205gwa"`, "bździągwa", "", ""},
		{`"b\305\272dzi\304\205gwa"`, "b\305\272dzi\304\205gwa", "", ""},
		{`"b\305dzi\304\205gwa"`, "b\305dzi\304\205gwa", "", ""}, // invalid utf-8 sequence, still ok
		{`"b\305"`, "b\305", "", ""},
		{`->`, "->", "", ""},
		{`-> `, "->", " ", ""},
		{`"with\nnewline"`, "with\nnewline", "", ""},
		{`"g\305\274e\ng\305\274\303\263\305\202ka"`, "g\305\274e\ng\305\274\303\263\305\202ka", "", ""},
		{`"quoted \" quote"`, `quoted " quote`, "", ""},
		{`""`, "", "", ""}, // TODO(mateuszc): test git if it can really store and print such filename
		// errors:
		{``, "", "", "cannot parse empty string as filename in git output"},
		{`"`, "", "", "cannot parse filename in git output: \""},
		{`"foo`, "", "", "cannot parse filename in git output: \"foo"},
		{`"foo\`, "", "", "cannot parse filename in git output (invalid syntax): \"foo\\"},
		{`"foo\"`, "", "", "cannot parse filename in git output: \"foo\\\""},
	}
	for _, c := range cases {
		filename, rest, err := git{}.parseFilename(c.input)
		if err != nil && err.Error() != c.expError {
			test.Errorf("case %q got unexpected error: %s", c, err)
		}
		if filename != c.expFilename {
			test.Errorf("case %q expected filename %q, got %q", c, c.expFilename, filename)
		}
		if rest != c.expRest {
			test.Errorf("case %q expected rest %q, got %q", c, c.expRest, rest)
		}
	}
}
