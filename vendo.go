package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// TODO(mateuszc): conform to kardianos/vendor-spec by preserving any unknown JSON fields in vendor.json

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

var cmds = &cobra.Command{
	Use: "vendo",
	// Long: "foo", // FIXME(mateuszc): write overall description of the tool
}

func run() error {
	// FIXME(mateuszc): make -v work also for any places where we use raw exec.Command
	cmds.Flags().BoolVar(&Verbose, "v", false, "show all executed commands")
	return cmds.Execute()
}
