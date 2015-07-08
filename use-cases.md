# Use cases plan for a custom vendoring tool

(for third-party Go packages)

1. User has some third-party pkgs already in GOPATH, non-vendored (i.e. outside the main repo), and wants to save their full source code
   ("vendor" them) into into *_vendor* subdir of the main repo, keeping information about original URL and revision-ids in a
   *[vendor.json](https://github.com/kardianos/vendor-spec)* file;
   1. Any *.git/.hg/.bzr* subdirs of the third-party pkgs should not be added into the main repo;
   2. Only those pkgs which are dependencies of the main repo should be saved; other pkgs present in *_vendor* (e.g. because user may
      develop with `GOPATH=$PROJ/_vendor;$PROJ`) should be "gitignored" by having "`/`" in `_vendor/.gitignore` file (cannot list each
      ignored pkg separately, because they may differ per user);
   3. A warning/error should be printed if some dependencies cannot be found in *_vendor* or GOPATH; (user must download them explicitly);
   4. *[Note]* Some pkgs may already be present in *_vendor*;
2. User clones the main repo from central server and wants to compile & test it;
   1. Compilation & testing should use the vendored pkgs (i.e. from *_vendor* subdir);
3. User pulls the new version of the main repo from central server and wants to compile & test it;
   1. *[Note]* Some packages may already exist in *_vendor* subdir (not tracked by Git) from earlier work, and/or because of earlier use of
      the vendoring tool;
4. User checkouts (via git) a different revision of the main repo and wants to compile & test it;
5. User wants to patch a repo in *_vendor* to fix a bug in a third-party repo;
   1. A *pre-commit* Git hook could detect that changes were made in a package, and require changing (adding or editing) the repo's
      `"comment"` field in the *vendor.json* file [a new revision-id would be desirable too, but it may not exist in the original repo, thus
      becoming nonsensical; also, old revision-id has advantage of keeping info about base commit; disadvantage is that *vendor.json* drops
      consistency with *_vendor* contents];
   2. *[Note]* The repo in *_vendor* may or may not have a *.git/.hg/.bzr* subdir;
6. User wants to update a third-party repo in *_vendor* from the Internet;
   1. *[Note]* The repo may or may not have a *.git/.hg/.bzr* subdir;
   2. *[Note]* The repo may be patched internally to fix a bug; it'd be desirable that this is detected and the update stopped;
7. User wants to change the code of the main repo, adding and removing some imports, then build & test, then commit the changes, then push
   them to the central server;
   1. A *pre-commit* hook should detect if new imports were added that are not present in *_vendor* (or some imports removed which are
      present there) and stop the commit (or just inform, without stopping?); still, user should be allowed to commit the code anyway if he
      really wants (*"--force"*);
   2. A tool must be available to auto-update (add & remove) packages in *_vendor* dir to satisfy the above *pre-commit* check; (still, we
      don't want to put the auto-update tool in *pre-commit* hook - we want user to run it explicitly, similar as with a *go fmt* hook);

This solution looks kinda costly to build now; but the main benefit it brings, is that the repo should become fully self-contained, and
especially all historic builds (since this solution is introduced) will be reproducible too, with correct versions of dependencies.
