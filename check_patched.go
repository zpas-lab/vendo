package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// CheckPatched checks that all packages imported by project are listed in the
// *vendor.json* file, and no others.
// (use-cases.md 7.1.1)
func CheckPatched() error {

	// NOTE: this function operates strictly on files in git's "staging area" (index).
	// ANY MODIFICATIONS MUST KEEP THIS INVARIANT.

	// NOTE: this function assumes CheckConsistency was already run and successful.

	// FIXME(mateuszc): write tests

	// Make sure we're in project's root dir (with .git/, vendor.json, and _vendor/)
	exist := Exist{}.Dir(".git").File(JsonPath).Dir(VendorPath)
	if exist.Err != nil {
		return exist.Err
	}

	// We want the "current on-disk" state of files to reflect the content of git's "staging area" ("index").  Because
	// that's what will be added in the subsequent git commit. And we want the vcs.IsClean() in subrepos to see that
	// content.
	stasher, err := GitStashUnstaged("vendo check-patched")
	if err != nil {
		return err
	}
	defer stasher.Unstash()

	// Check again after `git stash`
	exist = Exist{}.Dir(".git").File(JsonPath).Dir(VendorPath)
	if exist.Err != nil {
		return exist.Err
	}

	dirtyFiles, err := findDirtyStagedFiles()
	if err != nil {
		return err
	}
	if len(dirtyFiles) == 0 {
		return nil
	}

	// Find repos, based on ReadStagedVendorFile() & Tree{}
	// (use-cases.md 7.1.1.2)
	pkgs, err := ReadStagedVendorFile(JsonPath)
	if err != nil {
		return err
	}
	dirtyRoots, unmatchedFiles := pkgs.findReposOfFiles(dirtyFiles)
	if len(unmatchedFiles) > 0 && !reflect.DeepEqual(unmatchedFiles, []string{GitignorePath}) {
		// Show error message to user, listing all unmatched files except _vendor/.gitignore
		filtered := []string{}
		for _, file := range unmatchedFiles {
			if file != GitignorePath {
				filtered = append(filtered, file)
			}
		}
		return fmt.Errorf(`cannot find matching "repositoryRoot" in %s for following files: %s`,
			JsonPath, strings.Join(filtered, " "))
	}

	oldPkgs, err := ReadHeadVendorFile(JsonPath)
	if err != nil {
		return err
	}

	// (use-cases.md 7.1.1.3); more info in function's comment
	err = verifyCommentsForPatchedRepos(dirtyRoots, oldPkgs.ByRepositoryRoot(), pkgs.ByRepositoryRoot())
	if err != nil {
		return err
	}
	return nil
}

// findDirtyStagedFiles returns paths of all modified files added to git index
// ("staging area"). In other words, all files shown by `git status` as "Changes
// to be committed".
// (use-cases.md 7.1.1.1)
func findDirtyStagedFiles() ([]string, error) {
	// `git status --porcelain` => find staged files (line[0] not in " !?"); for rename, collect both file names
	// FIXME(mateuszc): test parsing of `git status --porcelain` for files with unicode chars & renaming a file named '->'
	// TODO(mateuszc): consider using a third-party git library. Known packages
	// in July 2015:
	// - https://github.com/libgit2/git2go
	//    (-) not pure Go: needs libgit2
	// - http://godoc.org/github.com/speedata/gogit
	//    (-) doesn't support index (staging area)
	// - http://godoc.org/github.com/gogits/git
	//    (-) doesn't support index (staging area)
	lines, err := Command("git", "status", "--porcelain", VendorPath).
		OutputLines()
	if err != nil {
		return nil, err
	}
	// Example git output (see "git help status" -> "Porcelain format" for
	// details).
	//
	//	 M bingo
	//	AD foobar
	//	R  "b\305\272dzi\304\205gwa" -> ->
	//	R  foo -> foz
	//	A  "g\305\274e\ng\305\274\303\263\305\202ka"
	//	A  baz/boo
	//	A  "with\nnewline"
	//	A  "with space"
	//	?? notrak
	dirtyFiles := []string{}
	for _, line := range lines {
		// Skip files not changed in index (staging area)
		status := line[0]
		if strings.IndexByte(" !?", status) != -1 {
			// First byte describes status of the file in staging area. If first
			// byte is ' ', it means the file was changed, but the change is not
			// staged, so we're not interested here and can ignore it. A '?'
			// means the file is untracked, so we ignore too. A '!' means file
			// is ignored by git.
			//
			// Any other value (e.g. 'R', 'M' or 'A') means we'll want to
			// collect the filename into dirtyFiles.
			continue
		}
		// Skip status info
		line = line[3:]
		// Parse file name, including with special chars and renames.
		filename, rest, err := git{}.parseFilename(line)
		if err != nil {
			return nil, err
		}
		dirtyFiles = append(dirtyFiles, filename)
		if rest == "" {
			continue
		}
		// Try to detect renames - they list another file which was changed too.
		suffix := strings.TrimPrefix(rest, " -> ")
		if suffix == rest {
			return nil, fmt.Errorf("unexpected format of git output: %q", line)
		}
		filename, rest, err = git{}.parseFilename(suffix)
		if err != nil {
			return nil, err
		}
		dirtyFiles = append(dirtyFiles, filename)
		if rest != "" {
			return nil, fmt.Errorf("unexpected format of git output: %q", line)
		}
	}
	return dirtyFiles, nil
}

// verifyCommentsForPatchedRepos checks all the repositories specified as
// repoRoots.  If any of them are detected as patched locally (vs. upstream,
// i.e. origin), the function verifies that the "comment" field was edited in
// vendor.json for corresponding packages (it should mention the patch).
// (use-cases.md 7.1.1.3)
func verifyCommentsForPatchedRepos(repoRoots set, oldByRepoRoot, newByRepoRoot map[string]*VendorPackage) error {
	// Iterate all repository roots with changes, and make sure that those changes are reflected in changed Comment.
	for root := range repoRoots {
		pkg := newByRepoRoot[root]
		if pkg == nil {
			return fmt.Errorf(`directory %s has a modified file, but does not match any "repositoryRoot" in %s`,
				root, JsonPath)
		}
		vcs, err := vcsList.IsRoot(root)
		if err != nil {
			return err
		}
		if vcs != nil {
			// If current Revision in subrepo (via git/hg/bzr) differs from Revision from *vendor.json*, report **error**.
			// (use-cases.md 7.1.1.3.1.1)
			diskRevision, err := vcs.Revision(root)
			if err != nil {
				return err
			}
			jsonRevision := pkg.Revision
			if diskRevision != jsonRevision {
				// TODO(mateuszc): improve the message to full format as in use-cases.md 7.1.1.3.1.1
				msg := `The revision in local repository at $PKG_REPO_ROOT:
  $PKG_LOCAL_REVISION $PKG_LOCAL_REV_DATE $PKG_LOCAL_REV_COMMENT
is inconsistent with information stored in 'vendor.json' for package $PKG:
  $PKG_REPO_REVISION $PKG_REPO_REV_DATE
  comment: $PKG_JSON_COMMENT
To fix the inconsistency, you are advised do one of the following actions,
depending on which is most appropriate in your case:
  a) revert $PKG_REPO_ROOT to $PKG_REPO_REVISION;
  b) update "revision" in 'vendor.json' to $PKG_LOCAL_REVISION;
  c) delete $PKG_REPO_ROOT/$VCS_DIR`
				msg = strings.NewReplacer(
					"$PKG_REPO_ROOT", pkg.RepositoryRoot,
					"$PKG_LOCAL_REVISION", diskRevision,
					"$PKG_LOCAL_REV_DATE", "", // TODO(mateuszc)
					"$PKG_LOCAL_REV_COMMENT", "", // TODO(mateuszc)
					"$PKG_REPO_REVISION", jsonRevision,
					"$PKG_REPO_REV_DATE", pkg.RevisionTime,
					"$PKG_JSON_COMMENT", pkg.Comment,
					"$PKG", pkg.Canonical,
					"$VCS_DIR", vcs.Dir(),
					"vendor.json", JsonPath,
				).Replace(msg)
				return errors.New(msg)
			}
			// Check if the subrepo is clean for the tested Revision.
			// (use-cases.md 7.1.1.3.1.2)
			// TODO(mateuszc): check files untracked in subrepo (but tracked in main repo) too?
			clean, err := vcs.IsClean(root, ".")
			if err != nil {
				return err
			}
			if clean {
				continue
			}
			// "fall through" to code below
		}
		// Repository is "dirty". Verify if Comment was changed (hopefully some info added) in vendor.json.
		// (use-cases.md 7.1.1.3.2)
		// FIXME(mateuszc): do this for all pkgs with the same RepositoryRoot
		oldPkg := oldByRepoRoot[root]
		switch {
		case oldPkg == nil:
			// New pkg added, apparently.
			if vcs != nil {
				return fmt.Errorf("sub-repository in: %s not clean in git index; please add pristine repository first, then add any local patches in separate commit later", pkg.RepositoryRoot)
			} else {
				return fmt.Errorf("cannot detect Version Control System in: %s", pkg.RepositoryRoot)
			}
		case oldPkg.Comment == pkg.Comment:
			return fmt.Errorf("local patch detected in: %s; please edit \"comment\" in %s to add note describing the patch", pkg.RepositoryRoot, JsonPath)
		}
	}
	return nil
}
