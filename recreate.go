package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runRecreate() error {
	// Use case "vendo-recreate"
	// (use-cases.md 1.)
	//
	// User adds pkgs from GOPATH to *_vendor* directory. User has some third-party pkgs already in GOPATH, non-vendored (i.e. outside the main
	// repo), and wants to save their full source code ("vendor" them) into into *_vendor* subdir of the main repo, keeping information about
	// original URL and revision-ids in a *[vendor.json](https://github.com/kardianos/vendor-spec)* file;
	//  1. Any *.git/.hg/.bzr* subdirs of the third-party pkgs should not be added into the main repo;
	//  2. Only those pkgs which are transitive dependencies of the main repo should be saved; other pkgs present in *_vendor* (e.g. because user
	//     may develop with `GOPATH=$PROJ/_vendor;$PROJ`) should be "gitignored" by having "`/`" in `_vendor/.gitignore` file (cannot list each
	//     ignored pkg separately, because they may differ per user);
	//  3. A warning/error should be printed if some dependencies cannot be found in *_vendor* or GOPATH; (user must download them explicitly);
	//  4. *[Note]* Some pkgs may already be present in *_vendor*;
	flags := flag.NewFlagSet("recreate", flag.ExitOnError)
	var (
		platformsList = flags.String("platforms", "", "format: OS_ARCH,OS_ARCH2[,...]")
		clone         = flags.Bool("clone", true, "if dependency doesn't exist in _vendor/, clone it from GOPATH")
	)

	flags.Parse(os.Args[2:])
	platforms, err := parsePlatforms(*platformsList)
	if err != nil {
		// TODO(mateuszc): subcmd usage
		return err
	}

	return Recreate(platforms, *clone)
}

func Recreate(platforms []Platform, clone bool) error {
	// Make sure we're in project's root dir (with .git)
	exist := Exist{}.Dir(".git")
	if exist.Err != nil {
		return exist.Err
	}

	// `mv vendor.json vendor.json.old`; (internally, *vendor.json.old* may exist only in memory, doesn't have to be created on disk);
	// (use-cases.md 1.5.1.2 - 1.5.1.3)
	pkgs, err := ReadVendorFile(JsonPath)
	if err != nil {
		return err
	}

	// "VENDO-FORGET"
	// (use-cases.md 1.5.1)

	err = forget()
	if err != nil {
		return err
	}

	// "VENDO-ADD"
	// (use-cases.md 1.5.2)

	os.MkdirAll(VendorPath, 0755) // Note: must be done before os.Stat(VendorPath)

	// Retrieve the main project's import path
	project, err := GoList("{{.ImportPath}}", ".").WithFailed().OutputOneLine()
	if err != nil {
		return err
	}

	imports, err := findImportsGreedily(project)
	if err != nil {
		return err
	}

	// Prepare new Environ with: GOPATH=$PWD/_vendor:$GOPATH
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// TODO(mateuszc): error if GOPATH is empty
	vendorAbsPath := filepath.Join(cwd, VendorPath)
	gopath := vendorAbsPath + string(filepath.ListSeparator) + os.Getenv("GOPATH")

	err = imports.addTransitiveDependencies(gopath, platforms)
	if err != nil {
		return err
	}

	err = imports.removeStdlibs(gopath)
	if err != nil {
		return err
	}

	missing, err := imports.findMissing(gopath)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("cannot find dependency packages: %s in GOPATH=%s\nTry running:\n\tgo get %s",
			missing, gopath, strings.Join(missing, " "))
	}

	err = writeVcsGitignore()
	if err != nil {
		return err
	}

	// Clone missing pkgs to _vendor/ from GOPATH
	// (use-cases.md 1.5.2.4.1)
	if clone {
		err := imports.cloneNonVendoredPackages(vendorAbsPath)
		if err != nil {
			return err
		}
	}

	// Verify that all dependency pkgs are now in _vendor/
	// (use-cases.md 1.5.2.4.2)
	missing, err = imports.findMissing(vendorAbsPath)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("cannot find the following packages in %s: %s",
			vendorAbsPath, missing)
	}

	pkgsNew, err := imports.buildVendorFile(pkgs.MapCanonical())
	if err != nil {
		return err
	}
	pkgsNew.Comment = pkgs.Comment

	err = gitAddPackages(pkgsNew.Packages)
	if err != nil {
		return err
	}

	// Write the new vendor.json, and add it to Git
	err = pkgsNew.WriteTo(JsonPath)
	if err != nil {
		return err
	}
	err = Command("git", "add", "--", JsonPath).DiscardOutput()
	if err != nil {
		return err
	}

	// VENDO-IGNORE

	err = modifyGitignoreFinal()
	if err != nil {
		return err
	}

	return nil
}

// Make Git "forget" the _vendor/ dir contents
// (use-cases.md 1.5.1.1)
func forget() error {
	// TODO(mateuszc): move this down, just before we start doing first "git add"?
	err := Command("git", "rm", "--cached", "-r", "--ignore-unmatch", "-q", VendorPath).DiscardOutput()
	if err != nil {
		return err
	}

	// Delete "_vendor/.gitignore".  We must do this to remove "/" line, which is expected to be present in "_vendor/.gitignore" as result
	// of "vendo-ignore" subcommand. Also, we want to do this to make sure we're starting with a "clean slate" - this simplifies logic of
	// "vendo-add", as it can now work in a purely additive fashion.
	// (use-cases.md 1.5.1.4)
	// TODO(mateuszc): move this down, just before we start doing first "git add"?
	err = os.Remove(GitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type set map[string]struct{}

func (s set) ToSlice() []string {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	return keys
}

type Imports set

// FIXME(mateuszc): why isn't ToSlice() "inherited" from type 'set'?
func (imports Imports) ToSlice() []string { return set(imports).ToSlice() }

// findImportsGreedily analyzes all "*.go" files (except `_*`, `.*`, `testdata`) for imports, regardless of GOOS and build tags.
// *[Note]* Just ignoring GOOS and GOARCH here is simpler than trying to parse & match them. As to build tags, we specifically want to
// cover all combinations of them, as we want to make sure *all ever* dependencies of our main project are found.
// (use-cases.md 1.5.2.1)
func findImportsGreedily(project string) (Imports, error) {
	fset := token.NewFileSet()
	imports := Imports{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, extError error) error {
		// Ignore: "testdata", "_*", ".*" (they're ignored by 'go build' too)
		name := info.Name()
		switch {
		case name == "testdata" || strings.HasPrefix(name, "_") || strings.HasPrefix(name, "."):
			if info.IsDir() && name != "." {
				return filepath.SkipDir
			}
			return nil
		case info.IsDir():
			return nil
		case filepath.Ext(name) != ".go":
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// TODO(mateuszc): more detailed error (with file & line) if necessary
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
		if file == nil {
			return nil
		}
		for _, quotedImp := range file.Imports {
			if quotedImp == nil || quotedImp.Path == nil {
				// TODO(mateuszc): warn
				continue
			}
			imp := strings.Trim(quotedImp.Path.Value, `"`)
			if hasImportPrefix(imp, project) {
				// Ignore in-project packages. We've already crawled all in-project pkgs for immediate dependencies, so
				// we can now work only on what's outside.
				// TODO(mateuszc): we should do this again before cloning, to protect if some external pkg depends back on the project
				continue
			}
			imports[imp] = struct{}{}
		}
		return nil
	})
	return imports, err
}

// addTransitiveDependencies builds a transitive list of import dependencies. If imported pkg is not found in GOPATH (including *_vendor*),
// then function reports **error**, and exit. To build the import list we use `go list`, because it handles build tags (we assume that we
// want all the imports built in "default" configuration, i.e. with no build tags). Finally, `go list` result depends on GOOS and GOARCH, so
// we merge result from every GOOS & GOARCH combination (as listed in `-platforms` **mandatory** argument).
// (use-cases.md 1.5.2.2)
func (imports Imports) addTransitiveDependencies(gopath string, platforms []Platform) error {
	for _, platform := range platforms {
		// Add all transitive dependencies reported by 'go list'.
		deps, err := GoList("{{range .Deps}}{{. | println}}{{end}}", imports.ToSlice()...).
			WithFailed().
			Setenv(
			"GOPATH="+gopath,
			"GOOS="+platform.Os,
			"GOARCH="+platform.Arch).
			OutputLines()
		if err != nil {
			return err
		}
		for _, imp := range deps {
			// TODO(mateuszc): path.Clean()? e.g. for: import "foo//bar"
			// FIXME(mateuszc): pkgs added here shouldn't "spill" to `go list` args for next platform
			imports[imp] = struct{}{}
		}
	}
	return nil
}

// removeStdlibs removes standard library packages from the list.
// go list -e -f '{{if .Standard}}{{.ImportPath}}{{end}}' pkg1 pkg2 ...
func (imports Imports) removeStdlibs(gopath string) error {
	stdlibs, err := GoList("{{if .Standard}}{{.ImportPath}}{{end}}", imports.ToSlice()...).
		WithFailed().
		Setenv(
		"GOPATH=" + gopath).
		OutputLines()
	if err != nil {
		return err
	}
	for _, stdlib := range stdlibs {
		delete(imports, stdlib)
	}
	return nil
}

// findMissing checks if there are any pkgs which can't be found in GOPATH (including _vendor/)
func (imports Imports) findMissing(gopath string) ([]string, error) {
	missing, err := GoList("{{if not .Root}}{{.ImportPath}}{{end}}", imports.ToSlice()...).
		WithFailed().
		Setenv(
		"GOPATH=" + gopath).
		OutputLines()
	if err != nil {
		return nil, err
	}
	return missing, nil
}

func (imports Imports) cloneNonVendoredPackages(toGopath string) error {
	// Find dependency pkgs which are not in _vendor/
	pending, err := GoList("{{if not .Root}}{{.ImportPath}}{{end}}", imports.ToSlice()...).
		WithFailed().
		Setenv(
		"GOPATH=" + toGopath).
		OutputLines()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		// `go list` won't handle empty list correctly, so we must quit explicitly.
		return nil
	}

	// Find where they are.
	// Note: go list will print different .Root than above, because we set different GOPATH environment variable
	lines, err := GoList("{{.ImportPath}}\t{{.Root}}", pending...).
		OutputLines()
	if err != nil {
		return err
	}

	// Do the cloning.
	completed := map[string]bool{}
	for i, line := range lines {
		// Parse `go list` output
		split := strings.SplitN(line, "\t", 2)
		if len(split) != 2 {
			return fmt.Errorf("cannot parse 'go list' line %d %q for cloning in:\n%s",
				i+1, line, strings.Join(lines, "\n"))
		}
		err := clonePackage(split[0], split[1], toGopath, completed)
		if err != nil {
			return err
		}
	}
	return nil
}

func clonePackage(importPath, fromGopath, toGopath string, skipRepos map[string]bool) error {
	// Find out imported pkg's repository root
	fromPackage := filepath.Join(fromGopath, "src", importPath)
	// TODO(mateuszc): make sure gopathImpDir is absolute?
	fromRepo, vcs, err := vcsList.FindRoot(fromPackage)
	switch {
	case err != nil:
		return err
	case vcs == nil || len(fromRepo) < len(fromGopath):
		return fmt.Errorf("cannot detect version control system for %s", fromPackage)
	case skipRepos[fromRepo]:
		return nil
	}

	// Do the actual cloning.
	// FIXME(mateuszc): make the below Println more user-friendly; ideally, print the executed command (?)
	fmt.Println("#", importPath, fromRepo, vcs.Dir())
	rel, err := filepath.Rel(fromPackage, fromRepo)
	if err != nil {
		return err
	}
	toRepo := filepath.Join(toGopath, "src", importPath, rel)
	err = os.MkdirAll(toRepo, 0755)
	if err != nil {
		return err
	}
	err = vcs.Clone(fromRepo, toRepo)
	if err != nil {
		return err
	}
	// FIXME(mateuszc): overwrite the final repo's "remote/origin URL" to the same as used in source repo, to facilitate 'go get -u'
	skipRepos[fromRepo] = true
	return nil
}

// buildVendorFile builds contents of new vendor.json file. It refreshes each dependency's
// revision-id & revision-date from repository (if available), or copies them
// from old vendor.json. If neither has it, reports error.
// (use-cases.md 1.5.2.4.4 - 1.5.2.4.5)
func (imports Imports) buildVendorFile(pkgsMap map[string]*VendorPackage) (VendorFile, error) {
	fmt.Println()
	// pkgsMap := pkgs.MapCanonical()
	pkgsNew := VendorFile{
		Tool: "github.com/zpas-lab/vendo",
	}
	for imp := range imports {
		vendorImpDir := filepath.Join(VendorPath, "src", imp)
		repoRoot, vcs, err := vcsList.FindRoot(vendorImpDir)
		if err != nil {
			return pkgsNew, err
		}
		if filepath.IsAbs(repoRoot) {
			panic(fmt.Sprintf("internal error: repoRoot [%q] is absolute [VendorPath=%q]",
				repoRoot, VendorPath))
		}

		pkg := pkgsMap[imp]
		if vcs == nil || repoRoot == "." {
			// Reuse old repo info if no repo found on disk.
			// TODO(mateuszc): instead of 'repoRoot=="."' above, `cd _vendor; FindRoot("src/"+imp)`
			// (use-cases.md 1.5.2.4.4.2)
			if pkg == nil {
				// FIXME(mateuszc): find if any parent dirs are in vendor.json
				return pkgsNew, fmt.Errorf("cannot find repository root for pkg %s either in %s/ or in %s",
					imp, VendorPath, JsonPath)
			}
			// TODO(mateuszc): update pkg.Local ?
		} else {
			// Update the repo info as needed
			// (use-cases.md 1.5.2.4.4.1)

			if pkg == nil {
				pkg = &VendorPackage{
					Canonical: imp,
				}
			}
			pkg.Local = vendorImpDir
			// RepositoryRoot should be OS-independent, so we use '/' as path separator
			pkg.RepositoryRoot = filepath.ToSlash(repoRoot)
			pkg.Revision, err = vcs.Revision(pkg.RepositoryRoot)
			if err != nil {
				return pkgsNew, err
			}
			pkg.RevisionTime, err = vcs.RevisionTime(pkg.RepositoryRoot)
			if err != nil {
				return pkgsNew, err
			}
		}

		pkgsNew.Packages = append(pkgsNew.Packages, pkg)
	}
	sort.Sort(PackagesOrder(pkgsNew.Packages))
	return pkgsNew, nil
}

// writeGitignore ensures that for any dependency repository, only its "snapshot"
// is stored in the main repository, without full repo metadata (history, branches,
// etc.) and without any "submodules" metadata.
// (use-cases.md 1.5.2.3)
func writeVcsGitignore() error {
	gitignore, err := os.OpenFile(GitignorePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer gitignore.Close()
	for _, vcs := range vcsList {
		_, err = fmt.Fprintln(gitignore, vcs.Dir())
		if err != nil {
			// FIXME(mateuszc): add more context to error msg
			return err
		}
	}
	// We need to know if file was really synced to disk properly, as it will be read by git in a subsequent command.
	err = gitignore.Close()
	if err != nil {
		// FIXME(mateuszc): add more context to error msg
		return err
	}
	return nil
}

// gitAddPackages adds contents of all dependency repositories to main project's repository.
// (use-cases.md 1.5.2.4.6)
func gitAddPackages(packages []*VendorPackage) error {
	added := map[string]bool{}
	for _, pkg := range packages {
		if added[pkg.RepositoryRoot] {
			continue
		}

		// Add the dependency repository to main project's repository.
		// NOTE(mateuszc): the trailing "/" seems to make a world of a difference (as of git 2.1.):
		// without "/", git seems to want to treat the dir as a submodule.
		err := Command("git", "add", "--", pkg.RepositoryRoot+"/").DiscardOutput()
		if err != nil {
			return err
		}
		added[pkg.RepositoryRoot] = true
	}
	return nil
}

// modifyGitignoreFinal makes sure that any other random pkgs in *_vendor* (i.e. which are not dependencies of the
// main project, but exist there e.g. because of user's GOPATH) are ignored by Git.
// (use-cases.md 1.5.3)
func modifyGitignoreFinal() error {
	gitignore, err := os.OpenFile(GitignorePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer gitignore.Close()
	_, err = fmt.Fprintf(gitignore, "/\n!.gitignore\n")
	if err != nil {
		// FIXME(mateuszc): add more context to error msg
		return err
	}
	// We need to know if file was really synced to disk properly, as it will be read by git in a subsequent command.
	err = gitignore.Close()
	if err != nil {
		// FIXME(mateuszc): add more context to error msg
		return err
	}
	err = Command("git", "add", GitignorePath).DiscardOutput()
	if err != nil {
		return err
	}
	return nil
}

func hasImportPrefix(imp, prefix string) bool {
	return imp == prefix || strings.HasPrefix(imp, prefix+"/")
}

type Platform struct{ Os, Arch string }

func parsePlatforms(platformsList string) ([]Platform, error) {
	if platformsList == "" {
		return nil, fmt.Errorf("non-empty '-platforms' argument must be provided")
	}
	platforms := []Platform{}
	for _, entry := range strings.Split(platformsList, ",") {
		// goos, goarch := splitOsArch(platform)
		split := strings.SplitN(entry, "_", 2)
		platform := Platform{
			Os:   split[0],
			Arch: "MISSING", // invalid, but we must handle bad OS & ARCH from user input anyway
		}
		if len(split) == 2 {
			platform.Os = split[1]
		}
		platforms = append(platforms, platform)
	}
	return platforms, nil
}
