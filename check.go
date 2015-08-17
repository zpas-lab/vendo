package main

import "github.com/spf13/cobra"

func init() {
	// User does normal coding in the main project. User wants to change the code of the main repo, adding and removing some imports, then build
	// & test, then commit the changes, then push them to the central server;
	// 1. A *pre-commit* hook should detect if new imports were added that are not present in *_vendor* (or some imports removed which are
	//    present there) and stop the commit (or just inform, without stopping?); still, user should be allowed to commit the code anyway if he
	//    really wants (*"--force"*);
	// (use-cases.md 6.1)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "[TODO][NIY] for use as a git pre-commit hook",
	}
	cmd.Run = wrapRun(func(cmd *cobra.Command, args []string) error {
		err := CheckConsistency()
		if err != nil {
			return err
		}
		err = CheckDependencies()
		if err != nil {
			return err
		}
		err = CheckPatched()
		if err != nil {
			return err
		}
		return nil
	})
	cmds.AddCommand(cmd)
}
