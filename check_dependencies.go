package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func CheckDependencies() error {
	// (use-cases.md 6.1.2.2)

	// NOTE: this function operates strictly on files in git's "staging area" (index).
	// ANY MODIFICATIONS MUST KEEP THIS INVARIANT.

	// FIXME(mateuszc): write tests

	// Make sure we're in project's root dir (with .git/, vendor.json, and _vendor/)
	exist := Exist{}.Dir(".git").File(JsonPath).Dir(VendorPath)
	if exist.Err != nil {
		return exist.Err
	}

	// (use-cases.md 6.1.2.2.1)
	stasher, err := GitStashUnstaged("vendo check-dependencies")
	if err != nil {
		return err
	}
	defer stasher.Unstash()

	// Check again after `git stash`
	exist = Exist{}.Dir(".git").File(JsonPath).Dir(VendorPath)
	if exist.Err != nil {
		return exist.Err
	}

	// Transitively find package dependencies.
	// (use-cases.md 6.1.2.2.2)
	project, err := findProjectImportPath()
	if err != nil {
		return err
	}
	imports, err := findImportsGreedily(project)
	if err != nil {
		return err
	}
	vendorAbsPath, err := getVendorAbsPath()
	if err != nil {
		return err
	}
	// Note: we don't need to merge with os.Getenv("GOPATH"). We still can find imports from outside _vendor/, only we won't get their
	// dependencies, but that's not crucial.
	gopath := vendorAbsPath
	pkgs, err := ReadStagedVendorFile(JsonPath)
	if err != nil {
		return err
	}
	err = imports.addTransitiveDependencies(gopath, pkgs.Platforms)
	if err != nil {
		return err
	}
	err = imports.removeStdlibs()
	if err != nil {
		return err
	}
	// TODO(mateuszc): delete again subpackages of 'project' from the list (in case some thirdparty imported in-project pkg)
	// (use-cases.md 6.1.2.2.3)
	detectedImports := imports.ToSlice()
	sort.Strings(detectedImports)

	// Verify that list of depdendencies is equal to list of packages in vendor.json.
	// (use-cases.md 6.1.2.2.4)
	jsonImports := []string{}
	for _, pkg := range pkgs.Packages {
		jsonImports = append(jsonImports, pkg.Canonical)
	}
	sort.Strings(jsonImports)
	if !reflect.DeepEqual(detectedImports, jsonImports) {
		jsonList := strings.Join(jsonImports, " ")
		detectedList := strings.Join(detectedImports, " ")
		return fmt.Errorf(`the list of packages in %[1]s differs from the list of dependencies in crawled disk files:
%[1]s:
	%s
crawled dependencies:
	%s
	%s
	`,
			JsonPath, jsonList, detectedList, mismatch(jsonList, detectedList))
	}

	return nil
}

func mismatch(a, b string) string {
	if len(a) > len(b) {
		a = a[:len(b)]
	}
	for i := range a {
		if a[i] != b[i] {
			return strings.Repeat(" ", i) + "↑"
		}
	}
	return strings.Repeat(" ", len(a)) + "↑"
}
