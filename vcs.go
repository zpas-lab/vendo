package main

import (
	"os"
	"path/filepath"
)

type VcsList []Vcs

var vcsList = VcsList{
	{
		Dir: ".git",
	},
	{
		Dir: ".hg",
	},
	{
		Dir: ".bzr",
	},
}

func (l VcsList) FindRoot(path string) (string, *Vcs, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}

	// Go up directory tree, until we find a subdir correct for one of the Vcs
	for {
		for _, vcs := range l {
			maybe := filepath.Join(abs, vcs.Dir)
			stat, err := os.Stat(maybe)
			switch {
			case os.IsNotExist(err):
				continue
			case err != nil:
				return "", nil, err
			case stat.IsDir():
				// FIXME(mateuszc): try refactoring to save abs (repo root) in the Vcs struct
				return abs, &vcs, nil
			}
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return "", nil, nil
}

// Vcs has information about a specific Version Control System (like git, Mercurial, SVN, ...).
type Vcs struct {
	Dir string
}
