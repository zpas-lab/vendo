package main

import "fmt"

type GoListCmd struct{ *Cmd }

func GoList(template string, imports ...string) GoListCmd {
	args := []string{"go", "list", "-f", template, "--"}
	args = append(args, imports...)
	return GoListCmd{Command(args[0], args[1:]...)}
}

func (cmd GoListCmd) WithFailed() GoListCmd {
	// Add "-e" to arguments, before "--"
	args := &cmd.Cmd.Cmd.Args
	for i := range *args {
		switch (*args)[i] {
		case "-e":
			return cmd
		case "--":
			before, mid, after := (*args)[:i], []string{"-e"}, (*args)[i:]
			*args = append(before, append(mid, after...)...)
			return cmd
		}
	}
	panic(fmt.Sprintf(`missing "--" in args: %q`, *args))
}
