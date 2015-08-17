package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
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

func wrapRun(run func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		err := run(cmd, args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}
