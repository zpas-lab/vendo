package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type GitStasher struct {
	stashCreated bool
}

// GitStashUnstaged hides from disk all unstaged changes (but doesn't touch
// untracked files). After the call, files on disk with mirror contents of the
// git index (staging area), plus any untracked files.
//
// BUG(mateuszc): As of git 2.1.0 (default in Ubuntu 14.04), when a file is
// added to git index and then deleted from disk, `git stash save --keep-index`
// + `git stash pop` recreates it on disk (`touch foobar && git add foobar &&
// rm foobar && git stash save --keep-index && git stash pop` -- file foobar
// exists on disk, although it shouldn't).
func GitStashUnstaged(comment string) (*GitStasher, error) {
	// NOTE(mateuszc): If no files are modified, `git stash save --keep-index`
	// exits successfully, but doesn't make a new stash, instead printing
	// "No local changes to save". Doing `git stash pop` later would be a bug.
	//
	// A workaround we employ is to capture the output of git stash, and detect
	// that either a stash is created (should have the 'comment' on first line
	// then), or the "No local changes to save" message is shown. This is ugly,
	// as it depends on messages from git which may change, but alternatives are
	// expensive. At least we try to protect against changed format of messages
	// or translated messages, by returning an error if we can't match the
	// message.
	//
	// Alternatives considered:
	// a) in GitUnstash, we could first check if the top stash has appropriate
	//    "comment" matching the one used in `git stash save`;
	//    * (-) someone could have created a stash with this name earlier;
	// b) we need `git stash save` only because of `go list` in
	//    vendo-check-dependencies; we could instead use package go/build and
	//    implement Context.OpenFile etc. to read from git index (staging area);
	//    * (-) much work and will be complex and hard to understand;
	//    * can use `git` commands to implement the funcs;
	//    * could use third-party Go git libraries, but the pure-Go ones I found
	//      don't seem to support index (staging area) handling :/
	//      * could implement it, but too much work:
	//        http://stackoverflow.com/q/4084921
	//      * http://godoc.org/github.com/speedata/gogit
	//      * http://godoc.org/github.com/gogits/git
	//      * [git2go - cgo wrapper for
	//        libgit2](https://godoc.org/github.com/libgit2/git2go)
	// c) don't call `git stash` at all; operate on working dir contents;
	//    * (-) this makes most of vendo-check-... non-robust;
	//    * (+) fast to implement;
	cmd := Command("git", "stash", "save", "--keep-index", comment)
	lines, err := cmd.
		LogAlways().
		OutputLines()
	if err != nil {
		return nil, err
	}
	switch {
	case len(lines) > 0 && strings.HasSuffix(lines[0], ": "+comment):
		return &GitStasher{stashCreated: true}, nil
	case len(lines) == 1 && lines[0] == "No local changes to save":
		return &GitStasher{stashCreated: false}, nil
	}
	return nil, fmt.Errorf("GitStashUnstaged cannot parse output of `%s`:\n%s",
		strings.Join(cmd.Cmd.Args, " "),
		strings.Join(lines, "\n"))
}

func (g *GitStasher) Unstash() {
	if !g.stashCreated {
		return
	}
	err := Command("git", "stash", "pop", "--quiet").
		LogAlways().
		DiscardOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vendo: INTERNAL ERROR: cannot undo git stash save! Unstaged modifications may be lost, sorry :(\n")
	}
}

func (git) parseFilename(line string) (filename, rest string, err error) {
	// FIXME(mateuszc): use this function in other places where parsing file names from git output
	// Should parse any of:
	//
	//	foo
	//	"b\305\272dzi\304\205gwa"
	//	->
	//	"with\nnewline"
	//	"g\305\274e\ng\305\274\303\263\305\202ka"

	if len(line) == 0 {
		return "", "", errors.New("cannot parse empty string as filename in git output")
	}

	if line[0] != '"' {
		pos := strings.Index(line, " ")
		if pos == -1 {
			return line, "", nil
		}
		// TODO(mateuszc): if pos==0 { return error }
		return line[:pos], line[pos:], nil
	}

	pending := line[1:]
	bytes := []byte{}
	for {
		if len(pending) == 0 {
			return "", "", fmt.Errorf("cannot parse filename in git output: %s", line)
		}
		if pending[0] == '"' {
			// Closing quote.
			return string(bytes), pending[1:], nil
		}
		// Below function properly decodes escape sequences like: \" \123 \n
		c, _, tail, err := strconv.UnquoteChar(pending, '"')
		if err != nil {
			return "", "", fmt.Errorf("cannot parse filename in git output (%s): %s", err, line)
		}
		// NOTE(mateuszc): git outputs UTF-8 as escaped bytes, e.g.: ą will be
		// printed as "\304\205". UnquoteChar will capture each as separate
		// "rune", because it expects Go-like string, where multibytes are not
		// escaped. So we must treat 'c' as byte, not as rune.
		bytes = append(bytes, byte(c))
		pending = tail
	}
}

// Note: this function is very primitive and limited. Both paths must be
// slash-only, and must have no trailing slashes. Must be either both relative,
// or both absolute. Disk is not accessed, so links are not checked. Comparison
// is case-sensitive.
func isSubdir(subdir, dir string) bool {
	return strings.HasPrefix(subdir, dir+"/")
}

func (g git) WalkStaged(root, subpath string, walkFunc filepath.WalkFunc) error {
	// FIXME(mateuszc): add comment noting that root is not prepended to 'subpath' passed to walkFunc
	// TODO(mateuszc): use "-z" option and parse appropriately - may be important for files with special chars like "\n"
	// TODO(mateuszc): optimization: use streaming interface via os/exec.Cmd.StdoutPipe()
	lines, err := g.command(root, "ls-files", "--stage").
		LogOnError().
		OutputLines()
	if err != nil {
		return fmt.Errorf("GitWalk: error running git ls-files: %s", err)
	}
	// FIXME(mateuszc): make sure 'subpath' is slash-only, non-absolute, clean
	// FIXME(mateuszc): handle properly subpath==""
	subpath = strings.TrimRight(subpath, "/") + "/"
	lastDir := ""
	skipDir := ""
	for _, line := range lines {
		filePath := strings.Split(line, "\t")[1]
		if !strings.HasPrefix(filePath, subpath) {
			continue
		}
		dir := filepath.Dir(filePath)
		if dir == skipDir || isSubdir(dir, skipDir) {
			continue
		}
		if dir != lastDir && !isSubdir(lastDir, dir) {
			lastDir = dir
			_, name := filepath.Split(dir)
			info := gitWalkInfo{
				name:  name,
				isDir: true,
			}
			err := walkFunc(dir, info, nil)
			if err == filepath.SkipDir {
				skipDir = dir
				continue
			}
			if err != nil {
				return err
			}
		}
		_, name := filepath.Split(filePath)
		info := gitWalkInfo{
			name:  name,
			isDir: false,
		}
		err := walkFunc(filePath, info, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

type gitWalkInfo struct {
	name  string
	isDir bool
}

func (g gitWalkInfo) IsDir() bool        { return g.isDir }
func (g gitWalkInfo) Name() string       { return g.name }
func (g gitWalkInfo) ModTime() time.Time { return time.Time{} }
func (g gitWalkInfo) Mode() os.FileMode  { return 0 } // FIXME(mateuszc): keep .IsDir contract
func (g gitWalkInfo) Size() int64        { return 0 }
func (g gitWalkInfo) Sys() interface{}   { return nil }

func (g git) show(root, object string) (io.ReadCloser, error) {
	// TODO(mateuszc): optimization: use streamed os/exec.Cmd.StdoutPipe()
	data, err := g.command(root, "show", object).
		LogNever().
		CombinedOutput()
	if err != nil {
		// TODO(mateuszc): is there a way we can more robustly detect it's really a missing file?
		return nil, &os.PathError{
			Op:   "git show",
			Path: object,
			Err:  os.ErrNotExist,
		}
	}
	return nopCloser{bytes.NewReader(data)}, nil
}

func (g git) ReadStaged(root, subpath string) (io.ReadCloser, error) {
	return g.show(root, ":"+subpath)
}
func (g git) ReadHead(root, subpath string) (io.ReadCloser, error) {
	return g.show(root, "HEAD:"+subpath)
}

type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }
