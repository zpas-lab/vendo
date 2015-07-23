package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IsClean returns true when a repository has no changes and no untracked files
// in subpath (relative to repository root).  Ignored files are not taken into
// account.
func (g git) IsClean(root, subpath string) (bool, error) {
	lines, err := Command("git", "--git-dir", filepath.Join(root, ".git"), "--work-tree", root, "status", "--porcelain", subpath).
		OutputLines()
	if err != nil {
		return false, err
	}
	return len(lines) == 0, nil
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

func (g git) ReadStaged(root, subpath string) (io.ReadCloser, error) {
	// TODO(mateuszc): optimization: use streamed os/exec.Cmd.StdoutPipe()
	data, err := g.command(root, "show", ":"+subpath).
		CombinedOutput()
	if err != nil {
		return nil, err
	}
	return nopCloser{bytes.NewReader(data)}, nil
}

type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }
