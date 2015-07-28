package main

import (
	"flag"
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
	// FIXME(mateuszc): make -v work also for any places where we use raw exec.Command
	verbose := flag.Bool("v", false, "show all executed commands")
	flag.Parse()

	if *verbose {
		Verbose = true
	}
	if flag.NArg() < 1 {
		return usage()
	}
	cmd := cmds[flag.Arg(0)]
	if cmd == nil {
		usage()
		return fmt.Errorf("unknown command: %s", flag.Arg(0))
	}
	return cmd()
}

const (
	JsonPath      = "vendor.json"
	VendorPath    = "_vendor"
	GitignorePath = VendorPath + "/.gitignore"
)
