package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	// FIXME(mateuszc): NIY
	panic("NIY")
}

var cmds = map[string]func() error{
	"recreate": runRecreate,
	"update":   runUpdate,
	"check":    runCheck,
}

func run() error {
	if len(os.Args) <= 1 {
		return usage()
	}
	cmd := cmds[os.Args[1]]
	if cmd == nil {
		usage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
	return cmd()
}

const (
	JsonPath      = "vendor.json"
	VendorPath    = "_vendor"
	GitignorePath = VendorPath + "/.gitignore"
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
		platforms = flags.String("platforms", "", "format: OS_ARCH,OS_ARCH2[,...]")
		clone     = flags.Bool("clone", true, "if dependency doesn't exist in _vendor/, clone it from GOPATH")
	)

	flags.Parse(os.Args[2:])
	if *platforms == "" {
		// TODO(mateuszc): subcmd usage
		return fmt.Errorf("non-empty '-platforms' argument must be provided")
	}

	// FIXME(mateuszc): make sure we're in project's root dir (with .git)

	// "VENDO-FORGET"
	// (use-cases.md 1.5.1)

	// Make Git "forget" the _vendor/ dir contents
	// (use-cases.md 1.5.1.1)
	// TODO(mateuszc): move this down, just before we start doing first "git add"?
	cmd := Command("git", "rm", "--cached", "-r", "--ignore-unmatch", "-q", VendorPath)
	_, err := cmd.OutputLines()
	if err != nil {
		return err
	}

	// `mv vendor.json vendor.json.old`; (internally, *vendor.json.old* may exist only in memory, doesn't have to be created on disk);
	// (use-cases.md 1.5.1.2 - 1.5.1.3)
	vendor, err := ReadVendorFile(JsonPath)
	if err != nil {
		return err
	}
	_ = vendor

	// Delete "_vendor/.gitignore".  We must do this to remove "/" line, which is expected to be present in "_vendor/.gitignore" as result
	// of "vendo-ignore" subcommand. Also, we want to do this to make sure we're starting with a "clean slate" - this simplifies logic of
	// "vendo-add", as it can now work in a purely additive fashion.
	// (use-cases.md 1.5.1.4)
	// TODO(mateuszc): move this down, just before we start doing first "git add"?
	err = os.Remove(GitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// "VENDO-ADD"
	// (use-cases.md 1.5.2)

	os.MkdirAll(VendorPath, 0755) // Note: must be done before os.Stat(VendorPath)

	// Retrieve the main project's import path
	project, err := Command("go", "list", "-e", ".").OutputOneLine()
	if err != nil {
		return err
	}

	// Analyze all "*.go" files (except `_*`, `.*`, `testdata`) for imports, regardless of GOOS and build tags.
	// *[Note]* Just ignoring GOOS and GOARCH here is simpler than trying to parse & match them. As to build tags, we specifically want to
	// cover all combinations of them, as we want to make sure *all ever* dependencies of our main project are found.
	// (use-cases.md 1.5.2.1)
	fset := token.NewFileSet()
	imports := map[string]struct{}{}
	err = filepath.Walk(".", func(path string, info os.FileInfo, extError error) error {
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

	// Prepare new Environ with: GOPATH=$PWD/_vendor:$GOPATH
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// TODO(mateuszc): error if GOPATH is empty
	vendorAbsPath := filepath.Join(cwd, VendorPath)
	gopath := vendorAbsPath + string(filepath.ListSeparator) + os.Getenv("GOPATH")

	// Build a transitive list of import dependencies. If imported pkg is not found in GOPATH (including *_vendor*), then report
	// **error**, and exit. To build the import list we use `go list`, because it handles build tags (we assume that we want all the
	// imports built in "default" configuration, i.e. with no build tags). Finally, `go list` result depends on GOOS and GOARCH, so we
	// merge result from every GOOS & GOARCH combination (as listed in `-platforms` **mandatory** argument).
	// (use-cases.md 1.5.2.2)
	for _, platform := range strings.Split(*platforms, ",") {
		goos, goarch := splitOsArch(platform)

		// Add all transitive dependencies reported by 'go list'.
		cmd := Command("go", "list", "-e", "-f", "{{range .Deps}}{{. | println}}{{end}}").Setenv(
			"GOPATH="+gopath,
			"GOOS="+goos,
			"GOARCH="+goarch,
		)
		for imp := range imports {
			cmd.Append(imp)
		}
		deps, err := cmd.OutputLines()
		if err != nil {
			return err
		}
		for _, imp := range deps {
			// TODO(mateuszc): path.Clean()? e.g. for: import "foo//bar"
			// FIXME(mateuszc): pkgs added here shouldn't "spill" to `go list` args for next platform
			imports[imp] = struct{}{}
		}
	}

	// Remove standard library packages from the list.
	// go list -e -f '{{if .Standard}}{{.ImportPath}}{{end}}' pkg1 pkg2 ...
	cmd = Command("go", "list", "-e", "-f", "{{if .Standard}}{{.ImportPath}}{{end}}").Setenv(
		"GOPATH=" + gopath,
	)
	for imp := range imports {
		cmd.Append(imp)
	}
	stdlibs, err := cmd.OutputLines()
	if err != nil {
		return err
	}
	for _, stdlib := range stdlibs {
		delete(imports, stdlib)
	}

	// Check if there are any pkgs which can't be found in GOPATH (including _vendor/)
	cmd = Command("go", "list", "-e", "-f", "{{if not .Root}}{{.ImportPath}}{{end}}").Setenv(
		"GOPATH=" + gopath,
	)
	for imp := range imports {
		cmd.Append(imp)
	}
	missing, err := cmd.OutputLines()
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("cannot find dependency packages: %s in GOPATH=%s\nTry running:\n\tgo get %s",
			missing, gopath, strings.Join(missing, " "))
	}

	// Clone missing pkgs to _vendor/ from GOPATH
	// (use-cases.md 1.5.2.3.1)
	if *clone {
		// Find dependency pkgs which are not in _vendor/
		cmd = Command("go", "list", "-e", "-f", "{{if not .Root}}{{.ImportPath}}{{end}}").Setenv(
			"GOPATH=" + vendorAbsPath,
		)
		for imp := range imports {
			cmd.Append(imp)
		}
		pending, err := cmd.OutputLines()
		if err != nil {
			return err
		}

		// Find where they are.
		// Note: go list will print different .Root than above, because we set different GOPATH environment variable
		lines, err := Command("go", "list", "-f", "{{.ImportPath}}\t{{.Root}}").
			Append(pending...).
			OutputLines()
		if err != nil {
			return err
		}

		// Do the cloning.
		for i, line := range lines {
			// Parse `go list` output
			split := strings.SplitN(line, "\t", 2)
			if len(split) != 2 {
				return fmt.Errorf("cannot parse 'go list' line %d %q for cloning in:\n%s",
					i+1, line, strings.Join(lines, "\n"))
			}
			imp := split[0]
			gopathRoot := split[1]

			// Find out imported pkg's repository root
			impDir := filepath.Join(gopathRoot, "src", imp)
			repoRoot, vcs, err := vcsList.FindRoot(impDir)
			if err != nil {
				return err
			}
			if vcs == nil {
				return fmt.Errorf("cannot detect version control system for %s", impDir)
			}

			// FIXME(mateuszc): implement the actual cloning
			fmt.Println(imp, repoRoot, vcs.Dir)

		}
	}

	fmt.Println(imports)
	// fmt.Println(pkgs)

	panic("NIY")
}

func hasImportPrefix(imp, prefix string) bool {
	return imp == prefix || strings.HasPrefix(imp, prefix+"/")
}

func splitOsArch(platform string) (string, string) {
	s := strings.SplitN(platform, "_", 2)
	if len(s) == 1 {
		// bad input, but we must handle bad OS & ARCH elsewhere anyway
		return s[0], "UNKNOWN"
	}
	return s[0], s[1]
}

func runUpdate() error {
	panic("NIY")
}

func runCheck() error {
	panic("NIY")
}
