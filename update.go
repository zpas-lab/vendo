package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func runUpdate() error {
	// Use case "vendo-update"
	//
	// User wants to update a third-party repo in *_vendor* from the Internet (i.e. `go get -u`);
	//  1. *[Note]* The repo may or may not have a *.git/.hg/.bzr* subdir; (no subdir e.g. when it was added by another user and pulled);
	//  2. *[Note]* The repo may be patched internally to fix a bug; it'd be desirable that this is detected and the update stopped;
	//  3. *[Note]* This will require updating all pkgs which have the same repo;
	flags := flag.NewFlagSet("update", flag.ExitOnError)
	var (
		force         = flags.Bool("f", false, "force package update even if it's not clean")
		deletePatch   = flags.Bool("delete-patch", false, "ignore local patches in the updated repository")
		platformsList = flags.String("platforms", "", "format: OS_ARCH,OS_ARCH2[,...]")
	)

	flags.Parse(os.Args[2:])
	if flags.NArg() != 1 {
		// TODO(mateuszc): subcmd usage
		return fmt.Errorf("subcommand 'update' requires argument specifying package import path")
	}
	updatedImp := flags.Arg(0)
	platforms, err := parsePlatforms(*platformsList)
	if err != nil {
		// TODO(mateuszc): subcmd usage
		return err
	}

	return Update(updatedImp, platforms, *force, *deletePatch)
}

func Update(updatedImp string, platforms []Platform, force, deletePatch bool) error {
	// Make sure we're in project's root dir (with .git/, vendor.json, and _vendor/)
	exist := Exist{}.Dir(".git").File(JsonPath).Dir(VendorPath)
	if exist.Err != nil {
		return exist.Err
	}

	pkgs, err := ReadVendorFile(JsonPath)
	if err != nil {
		return err
	}
	if pkgs == nil {
		return fmt.Errorf("file not found: %s", JsonPath)
	}
	updatedPkg := pkgs.ByCanonical()[updatedImp]
	switch {
	case updatedPkg == nil:
		return fmt.Errorf("import path %q not found in %s", updatedImp, JsonPath)
	case updatedPkg.RepositoryRoot == "":
		return fmt.Errorf(`empty or missing "repositoryRoot" for import path %s in %s`, updatedImp, JsonPath)
	case filepath.IsAbs(updatedPkg.RepositoryRoot):
		return fmt.Errorf(`"repositoryRoot": %q is absolute path (must be relative) for import path %s in %s`,
			updatedPkg.RepositoryRoot, updatedImp, JsonPath)
	case filepath.Clean(updatedPkg.RepositoryRoot) != updatedPkg.RepositoryRoot:
		return fmt.Errorf(`"repositoryRoot": %q is not a clean path (did you mean %q?) for import path %s in %s`,
			updatedPkg.RepositoryRoot, filepath.Clean(updatedPkg.RepositoryRoot), updatedImp, JsonPath)
	}

	// Delete *_vendor/.gitignore*.
	// This is required for `git status` calls and the final Recreate() call.
	// (use-cases.md 5.4.1.1)
	fmt.Fprintf(os.Stderr, "# rm -f %s\n", GitignorePath)
	err = os.Remove(GitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if !force {
		err := verifyCleanInProject(updatedPkg)
		if err != nil {
			return err
		}
	}

	// Delete the updated repository from disk, but keep it in git's memory.
	// `rm -rf _vendor/$PKG_REPO_ROOT`
	// (use-cases.md 5.4.1.3)
	fmt.Fprintf(os.Stderr, "# rm -rf %s\n", updatedPkg.RepositoryRoot)
	err = os.RemoveAll(updatedPkg.RepositoryRoot)
	if err != nil {
		return err
	}

	vendorAbsPath, err := getVendorAbsPath()
	if err != nil {
		return err
	}

	// Update the requested repository from the Internet, via `go get`.
	// FIXME(mateuszc): probably must run `go get -u` for each platform, to make sure all deps are fetched
	// NOTE(mateuszc): `go get -d` because some pkgs may be single-platform-only, we don't want to build them on bad platform
	// (use-cases.md 5.4.1.4)
	err = Command("go", "get", "-d", "--", updatedPkg.Canonical).
		Setenv(
		"GOPATH=" + vendorAbsPath).
		LogAlways().
		DiscardOutput()
	if err != nil {
		return err
	}

	if !deletePatch {
		err := verifyNotPatchedLocally(updatedPkg)
		if err != nil {
			return err
		}
	}

	// `vendo-recreate`;
	//  * *[Note]* Value of argument `-platforms` for *vendo-add* should be copied verbatim from mandatory argument `-platforms` of
	//    *vendo-update*;
	//  * *[Note]* This will update revision-id & revision-date for $PKG in *vendor.json*;
	//  * *[Note]* This will also add any new pkgs downloaded because they're dependencies of $PKG;
	// (use-cases.md 5.4.1.9)
	err = Recreate(platforms, false)
	if err != nil {
		return err
	}

	return nil
}

// verifyCleanInProject verifies if updated repository is clean, from perspective of the main repo.
// * *[Note]* We don't have to check `cd _vendor/$PKG_REPO_ROOT ; git/hg/bzr status`. If the files are "unmodified" from
//   perspective of the main repo, then it means they're at proper state for building the main project, regardless whether the
//   "subrepos" are consistent. Similarly, if they are "modified" from perspective of the main repo, this means some work was maybe
//   done in the main repo, and this is important to warn about.
// (use-cases.md 5.4.1.2)
func verifyCleanInProject(updatedPkg *VendorPackage) error {
	fmt.Fprintf(os.Stderr, "# git status %s\n", updatedPkg.RepositoryRoot)
	clean, err := git{}.IsClean(".", updatedPkg.RepositoryRoot)
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("repository at %s looks patched locally from upstream revision %s listed in %s",
			updatedPkg.RepositoryRoot, updatedPkg.Revision, JsonPath)
	}
	return nil
}

func verifyNotPatchedLocally(updatedPkg *VendorPackage) error {
	// Find repository root
	impDir := filepath.Join(VendorPath, "src", updatedPkg.Canonical)
	repoRoot, vcs, err := vcsList.FindRoot(impDir)
	if err != nil {
		return err
	}
	if vcs == nil || repoRoot == "." {
		return fmt.Errorf("cannot find repository root for %s", impDir)
	}
	if repoRoot != updatedPkg.RepositoryRoot {
		return fmt.Errorf("found repository root different than stored in %s: %q != %q",
			JsonPath, repoRoot, updatedPkg.RepositoryRoot)
	}

	// Remember for later the branch or revision used by *go get*.
	// (use-cases.md 5.4.1.5)
	symbolicRef, err := vcs.HeadSymbolicRef(repoRoot)
	if err != nil {
		return err
	}

	// Checkout the revision listed in vendor.json.
	// `(cd $PKG_REPO_ROOT; git/hg/bzr checkout $PKG_REPO_REVISION)`; if failed, **error**; ($PKG_REPO_REVISION comes from
	// *vendor.json* file);
	// (use-cases.md 5.4.1.6)
	if updatedPkg.Revision == "" {
		return fmt.Errorf(`empty "revision" for %s in %s`, updatedPkg.Canonical, JsonPath)
	}
	fmt.Fprintf(os.Stderr, "# cd %s ; vcs checkout %s\n", updatedPkg.RepositoryRoot, updatedPkg.Revision)
	err = vcs.Checkout(updatedPkg.RepositoryRoot, updatedPkg.Revision)
	if err != nil {
		return err
	}

	// Verify if the updated repository isn't patched locally after vendoring.
	// * *[Note]* We've done `rm` on the files, but we did NOT do `git rm` on them (in the main repo). So, after re-creating them, `git
	//   status` in the main repo should see the same files as before `rm`. So, it should conclude: "meh, nothing changed", i.e. `git
	//   status` is clean. If `git status` *does* show diff, this means our repo remembers something different (a "patch") than what we
	//   recreated based on revision-id listed in *vendor.json*. So, we must quit, and print an error message: "vendored pkg is patched
	//   locally; please merge manually".
	// (use-cases.md 5.4.1.7)
	fmt.Fprintf(os.Stderr, "# git status %s\n", updatedPkg.RepositoryRoot)
	clean, err := git{}.IsClean(".", updatedPkg.RepositoryRoot)
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("repository at %s looks patched locally from upstream revision %s listed in %s",
			updatedPkg.RepositoryRoot, updatedPkg.Revision, JsonPath)
	}

	// Return to the original branch or revision (as downloaded by *go get*).
	// `(cd $PKG_REPO_ROOT; git/hg/bzr checkout $GO_GET_REVISION)`;
	//  * *[Note]* We can't just `git checkout master`, because e.g. if tag 'go1' is present in repo, it is chosen by `go get` instead
	//    of 'master'.
	// (use-cases.md 5.4.1.8)
	fmt.Fprintf(os.Stderr, "# cd %s ; vcs checkout %s\n", updatedPkg.RepositoryRoot, symbolicRef)
	err = vcs.Checkout(updatedPkg.RepositoryRoot, symbolicRef)
	if err != nil {
		return err
	}
	return nil
}
