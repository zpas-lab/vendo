package main

import (
	"fmt"
	"os"
)

// TODO(mateuszc): conform to kardianos/vendor-spec by preserving any unknown JSON fields in vendor.json

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

func runUpdate() error {
	panic("NIY")
}

func runCheck() error {
	panic("NIY")
}
