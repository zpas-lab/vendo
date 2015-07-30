package main

import (
	"os"
	"path/filepath"
)

const (
	JsonPath      = "vendor.json"
	VendorPath    = "_vendor"
	GitignorePath = VendorPath + "/.gitignore"
)

func getVendorAbsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	vendorAbsPath := filepath.Join(cwd, VendorPath)
	return vendorAbsPath, nil
}
