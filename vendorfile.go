package main

import (
	"encoding/json"
	"os"
)

// Based on:
// https://github.com/kardianos/vendor-spec/blob/aedbf313488aa9887871048ddcc6f8a70ac02eab/README.md
// (commit from 2015.06.13)
//
// Extended with custom fields.
type VendorFile struct {
	// FIXME(mateuszc): add comment
	Tool string `json:"tool"`

	// Comment is free text for human use. Example "Revision abc123 introduced
	// changes that are not backwards compatible, so leave this as def876."
	Comment string `json:"comment,omitempty"`

	// Packages represents a collection of vendor packages that have been copied
	// locally. Each entry represents a single Go package.
	Packages []VendorPackage `json:"package"`
}

type VendorPackage struct {
	// Canonical import path. Example "rsc.io/pdf".
	// go get <Canonical> should fetch the remote package.
	Canonical string `json:"canonical"`

	// Package path relative to the vendor file.
	// Examples: "vendor/rsc.io/pdf".
	//
	// Local should always use forward slashes and must not contain the
	// path elements "." or "..".
	Local string `json:"local"`

	// The revision of the package. This field must be persisted by all
	// tools, but not all tools will interpret this field.
	// The value of Revision should be a single value that can be used
	// to fetch the same or similar revision.
	// Examples: "abc104...438ade0", "v1.3.5"
	Revision string `json:"revision"`

	// RevisionTime is the time the revision was created. The time should be
	// parsed and written in the "time.RFC3339" format.
	RevisionTime string `json:"revisionTime"`

	// Comment is free text for human use.
	Comment string `json:"comment,omitempty"`

	// RepositoryRoot is the package root repository. You can find repo metadata
	// directories here (.git, .hg, etc.)
	// Examples: "vendor/rsc.io/pdf".
	//
	// RepositoryRoot is custom field (specific for "vendo" tool). It must
	// always use forward slashes and must not contain the path elements "."
	// or "..".
	RepositoryRoot string `json:"repositoryRoot"`
}

func ReadVendorFile(path string) (*VendorFile, error) {
	r, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &VendorFile{}, nil
		}
		return nil, err
	}
	defer r.Close()

	data := VendorFile{}
	err = json.NewDecoder(r).Decode(&data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}
