package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckConsistency verifies that the following conditions are satisfied:
//
//	- all "repositoryRoot" directories listed in vendor.json exist in _vendor/;
//	- all the directories in _vendor/ belong to some "repositoryRoot";
//	- there are no stray files outside of repository roots in _vendor/ (other
//	  than _vendor/.gitignore).
//
// It doesn't check contents (files & dirs) of the repository roots - this is
// responsibility of func CheckPatched().
// (use-cases.md 6.1.2.1)
func CheckConsistency() error {

	// NOTE: this function operates strictly on files in git's "staging area" (index).
	// ANY MODIFICATIONS MUST KEEP THIS INVARIANT.

	// Make sure we're in project's root dir (with .git/, vendor.json, and _vendor/)
	exist := Exist{}.Dir(".git").File(JsonPath).Dir(VendorPath)
	if exist.Err != nil {
		return exist.Err
	}

	// Parse *vendor.json*, sort by pkg path.
	// (use-cases.md 6.1.2.1.2)
	pkgs, err := ReadStagedVendorFile(JsonPath)
	if err != nil {
		return err
	}
	if pkgs == nil {
		return fmt.Errorf("file not found: %s", JsonPath)
	}

	// Build a tree from RepositoryRoots
	repoRoots := Tree{}
	unvisitedRoots := set{}
	for _, p := range pkgs.Packages {
		unvisitedRoots.Add(p.RepositoryRoot)
		err := repoRoots.Put(p.RepositoryRoot)
		if err != nil {
			return err
		}
	}

	// We want to check that all RepositoryRoots from vendor.json (from index) are in git (index), and that there are no files in _vendor/
	// out of RepositoryRoots (except _vendor/.gitignore).
	// Note: we're not interested in files under RepositoryRoots (they will be checked by CheckPatched()).
	// (use-cases.md 6.1.2.1.3)
	// NOTE(mateuszc): we walk only dirs known to git; there may be untracked dirs, we're not interested in them
	err = git{}.WalkStaged(".", VendorPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == GitignorePath {
			return nil
		}
		expected := repoRoots.Get(path)
		if expected == nil {
			return fmt.Errorf("unexpected file/directory in git, but not in %s: %s", JsonPath, path)
		}
		if !info.IsDir() {
			return fmt.Errorf("unexpected file in git, not in any %s repositoryRoot: %s", JsonPath, path)
		}
		if expected.IsEmpty() { // repository root
			// FIXME(mateuszc): if has *.git/.hg/.bzr* subdir, then verify revision-id match with *vendor.json*; if failed, report **error**
			// (see *vendo-check-patched* for error message details); (use-cases.md 6.1.2.1.3.3)
			delete(unvisitedRoots, path)
			return filepath.SkipDir
		} else {
			return nil
		}
	})
	if err != nil {
		return err
	}
	// if any pkg in *vendor.json* is not visited, then report **error**;
	// (use-cases.md 6.1.2.1.4)
	if len(unvisitedRoots) > 0 {
		return fmt.Errorf("following %s repositoryRoots not found in git: %s", JsonPath, strings.Join(unvisitedRoots.ToSlice(), " "))
	}

	// TODO(mateuszc): check that any *.git/.hg/.bzr* subdirs, if present, are at locations noted in $PKG_REPO_ROOT fields;
	return nil
}

type Tree map[string]Tree

func (t Tree) Put(path string) error {
	// FIXME(mateuszc): also add checks for path cleanness, slash-ness, absolute paths or not in _vendor/src
	if path == "" {
		// TODO(mateuszc): make the error msg here more generic, add vendor-related context in caller
		return fmt.Errorf(`empty "repositoryRoot" for import %s in %s`, path, JsonPath)
	}
	path = filepath.ToSlash(path)
	segments := strings.Split(path, "/")
	subtree := t
	for _, seg := range segments {
		if subtree[seg] == nil {
			subtree[seg] = Tree{}
		}
		subtree = subtree[seg]
	}
	return nil
}

func (t Tree) Get(path string) Tree {
	// FIXME(mateuszc): handling of path=="" (and maybe "." too?)
	segments := strings.Split(path, "/")
	subtree := t
	for _, seg := range segments {
		subtree = subtree[seg]
		if subtree == nil {
			return nil
		}
	}
	return subtree
}

func (t Tree) IsEmpty() bool { return len(t) == 0 }
