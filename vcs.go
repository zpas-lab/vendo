package main

import (
	"os"
	"path/filepath"
	"time"
)

type VcsList []Vcs

var vcsList = VcsList{
	git{},
	mercurial{},
	bazaar{},
}

// Vcs has information about a specific Version Control System (like git, Mercurial, SVN, ...).
type Vcs interface {
	// TODO(mateuszc): change "Dir" to "func IsRoot(path string) bool"
	Dir() string
	// Clone copies a repository between specified directories. The to directory must exist and be empty.
	Clone(from, to string) error
	Revision(root string) (string, error)
	RevisionTime(root string) (string, error)
	// HeadSymbolicRef attempts to retrieve a symbolic name of the currently
	// checked out revision (e.g. branch or tag name). If not possible, it
	// returns the same result as Revision.
	HeadSymbolicRef(root string) (string, error)
	Checkout(root, revision string) error
	// IsClean returns true when a repository has no changes and no untracked
	// files in subpath (relative to repository root).  Ignored files are not
	// taken into account.
	IsClean(root, subpath string) (bool, error)
}

type git struct{}

func (git) command(root string, args ...string) *Cmd {
	return Command("git", "--git-dir", filepath.Join(root, ".git")).Append(args...)
}
func (git) Dir() string {
	return ".git"
}
func (git) Clone(from, to string) error {
	return Command("git", "clone", "--", from, to).DiscardOutput()
}
func (g git) Revision(root string) (string, error) {
	return g.command(root, "rev-parse", "HEAD").OutputOneLine()
}
func (git) RevisionTime(root string) (string, error) {
	// FIXME(mateuszc): verify that timeFormat is correct for %aD, or
	// use different git format
	return vcsRevisionTime("Mon, 2 Jan 2006 15:04:05 -0700",
		"git", "--git-dir", filepath.Join(root, ".git"), "log", "-1", "--pretty=format:%aD")
}
func (g git) HeadSymbolicRef(root string) (string, error) {
	line, err := g.command(root, "symbolic-ref", "-q", "--short", "HEAD").
		LogNever().
		OutputOneLine()
	if err == nil {
		return line, nil
	} else {
		return g.command(root, "rev-parse", "HEAD").OutputOneLine()
	}
}
func (g git) Checkout(root, revision string) error {
	return g.command(root, "--work-tree", root, "checkout", revision).DiscardOutput()
}
func (g git) IsClean(root, subpath string) (bool, error) {
	// TODO(mateuszc): what if filepath.IsAbs(subpath)==true?
	lines, err := g.command(root, "--work-tree", root, "status", "--porcelain", subpath).
		OutputLines()
	if err != nil {
		return false, err
	}
	return len(lines) == 0, nil
}

type mercurial struct{}

func (mercurial) Dir() string {
	return ".hg"
}
func (mercurial) Clone(from, to string) error {
	return Command("hg", "clone", "--", from, to).DiscardOutput()
}
func (mercurial) Revision(root string) (string, error) {
	return Command("hg", "-R", root, "parent", "--template", "{node}").OutputOneLine()
}
func (mercurial) RevisionTime(root string) (string, error) {
	return vcsRevisionTime(time.RFC3339,
		"hg", "-R", root, "parent", "--template", "{date | rfc3339date}")
}
func (mercurial) HeadSymbolicRef(root string) (string, error) {
	// FIXME(mateuszc): try to retrieve proper "symbolic-ref"
	return mercurial{}.Revision(root)
}
func (mercurial) Checkout(root, revision string) error {
	// FIXME(mateuszc): make sure if we're ok to use -R or we should use --cwd
	return Command("hg", "-R", root, "update", revision).DiscardOutput()
}
func (mercurial) IsClean(root, subpath string) (bool, error) {
	lines, err := Command("hg", "-R", root, "status", filepath.Join(root, subpath)).
		OutputLines()
	if err != nil {
		return false, err
	}
	return len(lines) == 0, nil
}

type bazaar struct{}

func (bazaar) Dir() string {
	return ".bzr"
}
func (bazaar) Clone(from, to string) error {
	// FIXME(mateuszc): verify that 'to' is a dir before removing
	err := os.Remove(to)
	if err != nil {
		return err
	}
	return Command("bzr", "clone", "--", from, to).DiscardOutput()
}
func (bazaar) Revision(root string) (string, error) {
	return Command("bzr", "version-info", "--custom", "--template", "{revision_id}", root).OutputOneLine()
}
func (bazaar) RevisionTime(root string) (string, error) {
	// Bzr date format seems to use "+0000", not "Z", for GMT, see:
	// http://doc.bazaar.canonical.com/beta/en/user-guide/version_info.html
	return vcsRevisionTime("2006-01-02 15:04:05 -0700",
		"bzr", "version-info", "--custom", "--template", "{date}", root)
}
func (bazaar) HeadSymbolicRef(root string) (string, error) {
	// FIXME(mateuszc): try to retrieve proper "symbolic-ref"
	return bazaar{}.Revision(root)
}
func (bazaar) Checkout(root, revision string) error {
	return Command("bzr", "update", "-r", "revid:"+revision, root).DiscardOutput()
}
func (bazaar) IsClean(root, subpath string) (bool, error) {
	// TODO(mateuszc): test this
	path := filepath.Join(root, subpath)
	cmds := []*Cmd{
		Command("bzr", "modified"),
		Command("bzr", "deleted"),
		Command("bzr", "added"),
		Command("bzr", "ls", "--unknown"),
	}
	for _, cmd := range cmds {
		cmd.Append("-d", path)
		lines, err := cmd.OutputLines()
		if err != nil {
			return false, err
		}
		if len(lines) > 0 {
			return false, nil
		}
	}
	return true, nil
}

func vcsRevisionTime(timeFormat, command string, args ...string) (string, error) {
	line, err := Command(command, args...).OutputOneLine()
	if err != nil {
		return "", err
	}
	t, err := time.Parse(timeFormat, line)
	if err != nil {
		// FIXME(mateuszc): add more context to the error message
		return "", err
	}
	return t.Format(time.RFC3339), nil
}

// FindRoot goes up the directory tree starting from path, looking for the root
// directory of any repository of type listed in l.
//
// Note: if path is relative, the search doesn't go further up the directory
// tree than allowed by the path.
func (l VcsList) FindRoot(path string) (string, Vcs, error) {
	// Go up directory tree, until we find a subdir correct for one of the Vcs
	// TODO(mateuszc): possible optimization: try calling each vcs only once to
	// report its root, if possible (e.g. for git: `git rev-parse
	// --show-toplevel`), then return the longest (?)
	for {
		vcs, err := l.IsRoot(path)
		if err != nil {
			return "", nil, err
		}
		if vcs != nil {
			return path, vcs, nil
		}

		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return "", nil, nil
}

func (l VcsList) IsRoot(path string) (Vcs, error) {
	for _, vcs := range l {
		maybe := filepath.Join(path, vcs.Dir())
		stat, err := os.Stat(maybe)
		switch {
		case os.IsNotExist(err):
			continue
		case err != nil:
			return nil, err
		case stat.IsDir():
			// FIXME(mateuszc): try refactoring to save path (repo root) in the Vcs struct
			return vcs, nil
		}
	}
	return nil, nil
}
