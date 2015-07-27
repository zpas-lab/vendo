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
