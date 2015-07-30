# Use cases plan for a custom vendoring tool

(for third-party Go packages)

**NOTE:** Try stealing as much stuff as possible from http://github.com/skelterjohn/wgo; it seems to be the tool most similar to our plan
from what's out there (relatively; absolutely, it's not very similar). Also look at the other tools and try stealing too if they have some
worthy chunks of code. (As long as license is OK for us.)

**NOTE:** The tool will store an additional field `"repositoryPath"` in the *vendor.json* file; this is allowed by
*[vendor-spec](https://github.com/kardianos/vendor-spec)*. In initial versions, for easier coding, it also won't retain any unknown fields
in *vendor.json*; this is an **incompatibility** with *vendor-spec*. TODO: [LATER] improve it be compatible with *vendor-spec* (retain the
unknown fields).

**NOTE:** All the *pre-commit* hooks described below (i.e. _vendo-check-*_ commands) are assumed to check only "what is git-added" ("index"
in git parlance?), vs. what's in previous commit. Because that is what's going to make the contents of the new commit. In other words, each
of the checks should be wrapped with the following *bash* lines: `git stash -q --keep-index; trap 'git stash pop -q' EXIT` (see: [Tips for
using pre-commit hook](http://codeinthehole.com/writing/tips-for-using-a-git-pre-commit-hook/)). Unless the hook is carefully written in
such way that it's indifferent to that (i.e. only compares "staged" files to "previous commit" files), and then it should state so in usage
information. This also means that the hooks shall ignore git untracked & unstaged files.

**TODO:** What if vendored repos have Git submodules? At least we should detect such situation, so that we can then stop and look carefully
if our tool works acceptably, or not.

**TODO:** [LATER] Also support vendoring specific commandline tools (e.g. go2xunit) with dependencies. But not now, we don't actually need
it at the moment.

**TODO:** To make some operations simpler, and all of the idea overally, maybe require that all the *.git/.hg/.bzr* subdirs in *_vendor*
MUST be deleted? (and do this deletion in *vendo-add*);
  - (+) simpler regular operation and many commands;
  - (-) *vendo-update* somewhat harder for user: he/she cannot easily verify & browse the subrepo's history;
    - but this should really be needed only during the update; afterwards, after new version is fully committed into the main repo, he/she
      shouldn't need to browse history anymore;
  - (-) may be harder to introduce git submodules in future; (or not? consider YAGNI)
  - (-) if someone has something important in this *.git/...* repo locally, it would be lost;
  - (+?) maybe then we can reuse bigger parts of http://github.com/skelterjohn/wgo ?
  - actually, we already don't use *.git/...* much: only really when needed, otherwise we just check it as an additional safeguard (in the
    _vendo-check-*_ commands);

List of subcommands of the vendo tool, as planned in below points (specific names are not final, can be changed):

    vendo-recreate  # internal subcommands: (vendo-forget; foreach GOOS,GOARCH {vendo-add}; vendo-ignore)
    vendo-update
    vendo-check-patches
    vendo-check-consistency
    vendo-check-dependencies

Example directory structure of a project using the vendo tool, on user's local disk (checkouted):

    .git/                #   - main project's repository metadata
    libfoo/              # \
      foo.go             #  |
    cmd/                 #  |- main project's source code
      fum/               #  |
        main.go          # /
    vendor.json          # \
    _vendor/             #  |- managed by 'vendo' tool; checked-in to main repo
      .gitignore         #  |
      src/               # /
        github.com/
          bradfitz/
            iter/        #   - imported by libfoo/foo.go; checked-in to main repo
              .git/      # NOTE: .git/ not checked-in to main repo; listed in _vendor/.gitignore
              iter.go
          rsc/
            c2go/        # \   NOTE:
              .git/      #  |- not imported by main project => NOT CHECKED-IN to main repo,
              main.go    # /   fully ignored because of "/" in _vendor/.gitignore
        labix.org/
          v2/
            mgo/         #   - imported by libfoo/foo.go; checked-in to main repo
              .bzr/      # NOTE: .bzr/ not checked-in to main repo; listed in _vendor/.gitignore
              mgo.go
        code.google.com/
          p/
            gofpdf/      #   - imported by libfoo; checked-in to main repo
              gofpdf.go  # NOTE: no .hg/.bzr/.git directory

1. User adds pkgs from GOPATH to *_vendor* directory. User has some third-party pkgs already in GOPATH, non-vendored (i.e. outside the main
   repo), and wants to save their full source code ("vendor" them) into into *_vendor* subdir of the main repo, keeping information about
   original URL and revision-ids in a *[vendor.json](https://github.com/kardianos/vendor-spec)* file;
   1. Any *.git/.hg/.bzr* subdirs of the third-party pkgs should not be added into the main repo;
   2. Only those pkgs which are transitive dependencies of the main repo should be saved; other pkgs present in *_vendor* (e.g. because user
      may develop with `GOPATH=$PROJ/_vendor;$PROJ`) should be "gitignored" by having "`/`" in `_vendor/.gitignore` file (cannot list each
      ignored pkg separately, because they may differ per user);
   3. A warning/error should be printed if some dependencies cannot be found in *_vendor* or GOPATH; (user must download them explicitly);
   4. *[Note]* Some pkgs may already be present in *_vendor*;
   5. **IMPLEMENTATION** - *vendo-recreate -platforms=linux_amd64,darwin_amd64[,...]*:
      1. internal subcommand `vendo-forget`:
         1. `git 'forget' _vendor`;
         2. `mv vendor.json vendor.json.old`; (internally, *vendor.json.old* may exist only in memory, doesn't have to be created on disk);
         3. `rm vendor.json`;
         4. `rm _vendor/.gitignore`;
            * *[Note]* We must do this to remove a "`/`", which should be present in *_vendor/.gitignore* as result of *vendo-ignore*
              command. Also, we want to do this to make sure we're starting with a "clean slate" - this simplifies logic of *vendo-add*, as
              it can now work in a purely additive fashion;
      2. internal subcommand `vendo-add -platforms=linux_amd64,darwin_amd64[,...] [./...]`;
         1. analyze all \*.go files (except `_*`, `.*`, `testdata`) for imports, regardless of GOOS and build tags;
            * *[Note]* Just ignoring GOOS and GOARCH here is simpler than trying to parse & match them. As to build tags, we specifically
              want to cover all combinations of them, as we want to make sure *all ever* dependencies of our main project are found.
            * *[Note]* In this step only, we don't want to use `go list`, but a custom Go parser. That's because we want to "greedily" find
              any possible imports for any possible combinations of build tags.
         2. build a transitive list of import dependencies. If imported pkg is not found in GOPATH (including *_vendor*), then report
            **error**, and exit. To build the import list we use `go list`, because it handles build tags (we assume that we want all the
            third-party/external imports built in "default" configuration, i.e. with no build tags). Finally, `go list` result depends on
            GOOS and GOARCH, so we merge result from every GOOS & GOARCH combination (as listed in `-platforms` **mandatory** argument).
         3. add `.git` (and `.hg`, `.bzr`) to *_vendor/.gitignore*;
         4. for each dependency pkg:
            1. if not present in *_vendor*, but present in GOPATH, `git/hg/bzr clone $GOPATH_REPO _vendor/$PKG_REPO_ROOT` (unless option
               `--clone=false` is provided), and copy the source repo's origin URL to target repo (e.g. `cd $PKG_REPO_ROOT; git remote set
               origin $REPO_URL`);
            2. if not present in *_vendor* afterwards, report **error**, os.Exit(1);
            3. pkg is now for sure present in *_vendor*;
            4. "update revision-id & revision-date":
               1. if has *.git/.hg/.bzr* subdir, update *vendor.json* revision-id & revision-date;
               2. else if pkg not present in *vendor.json.old*, then **error**: "cannot detect repo type";
            5. add pkg to *vendor.json*, keeping any fields from *vendor.json.old* (including "comment", "revision", "revisionDate");
            6. `git add _vendor/$PKG_REPO_ROOT`;
       3. internal subcommand `vendo-ignore`; -- makes sure that any other random pkgs in *_vendor* (i.e. which are not dependencies of the
          main project, but exist there e.g. because of user's GOPATH) are ignored by Git;
          1. `echo / >> _vendor/.gitignore`;
          2. `git add _vendor/.gitignore`;
2. User clones the main repo from central server and wants to compile & test it;
   1. Compilation & testing should use the vendored pkgs (i.e. from *_vendor* subdir);
   2. **IMPLEMENTATION**:
      1. `git clone ...`
      2. `GOPATH=$PROJ/_vendor;$OLD_GOPATH` -- possibly with a helper tool: `GOPATH=$(vendo-gopath)`;
      3. `go build ./... ; go test ./...` etc.;
3. User pulls the new version of the main repo from central server and wants to compile & test it;
   1. *[Note]* Some packages may already exist in *_vendor* subdir (not tracked by Git) from earlier work, and/or because of earlier use of
      the vendoring tool;
   2. **IMPLEMENTATION**:
      1. as in usecase above;
4. User checkouts (via git) a different revision of the main repo and wants to compile & test it;
   1. **IMPLEMENTATION**:
      1. as in usecase above;
5. User wants to update a third-party repo in *_vendor* from the Internet (i.e. `go get -u`);
   1. *[Note]* The repo may or may not have a *.git/.hg/.bzr* subdir; (no subdir e.g. when it was added by another user and pulled);
   2. *[Note]* The repo may be patched internally to fix a bug; it'd be desirable that this is detected and the update stopped;
   3. *[Note]* This will require updating all pkgs which have the same repo;
   4. **IMPLEMENTATION**:
      1. `vendo-update [-platforms=linux_amd64,darwin_amd64[,...]] PKG`;
         1. `rm _vendor/.gitignore`; (required for a `vendo-recreate` step below and for `git status` calls);
         2. if `git status _vendor/$PKG_REPO_ROOT` shows diff, then **error** (unless `-f`|`--force` option provided);
            * *[Note]* We don't have to check `cd _vendor/$PKG_REPO_ROOT ; git/hg/bzr status`. If the files are "unmodified" from
              perspective of the main repo, then it means they're at proper state for building the main project, regardless whether the
              "subrepos" are consistent. Similarly, if they are "modified" from perspective of the main repo, this means some work was maybe
              done in the main repo, and this is important to warn about.
         3. `rm -rf _vendor/$PKG_REPO_ROOT`;
         4. `GOPATH=_vendor go get $PKG`; if failed, **error** (don't revert; user can retry with `-f` option);
             * what if the pkg is in "external" GOPATH? (i.e. out of *_vendor*);
               * setting `GOPATH=_vendor` (instead of earlier proposed `GOPATH=_vendor;$GOPATH`) should fix this issue;
         5. Remember for later the branch or revision used by *go get*:
            `(cd $PKG_REPO_ROOT; git symbolic-ref -q --short HEAD || git rev-parse HEAD`; - store the output in $GO_GET_REVISION;
         6. `(cd $PKG_REPO_ROOT; git/hg/bzr checkout $PKG_REPO_REVISION)`; if failed, **error**; ($PKG_REPO_REVISION comes from
            *vendor.json* file);
         7. if `git status _vendor/$PKG_REPO_ROOT` shows diff, then **error** (this means that the repo was patched locally after
            vendoring), unless `--delete-patch` option provided;
            * *[Note]* We've done `rm` on the files, but we did NOT do `git rm` on them (in the main repo). So, after re-creating them, `git
              status` in the main repo should see the same files as before `rm`. So, it should conclude: "meh, nothing changed", i.e. `git
              status` is clean. If `git status` *does* show diff, this means our repo remembers something different (a "patch") than what we
              recreated based on revision-id listed in *vendor.json*. So, we must quit, and print an error message: "vendored pkg is patched
              locally; please merge manually".
         8. `(cd $PKG_REPO_ROOT; git/hg/bzr checkout $GO_GET_REVISION)`;
             * *[Note]* We can't just `git checkout master`, because e.g. if tag 'go1' is present in repo, it is chosen by `go get` instead
               of 'master'.
         9. `vendo-recreate`;
             * *[Note]* Value of argument `-platforms` for *vendo-add* should be copied verbatim from argument `-platforms` of
               *vendo-update*, or read from *vendor.json* custom global field "platforms" otherwise;
             * *[Note]* This will update revision-id & revision-date for $PKG in *vendor.json*;
             * *[Note]* This will also add any new pkgs downloaded because they're dependencies of $PKG;
6. User does normal coding in the main project. User wants to change the code of the main repo, adding and removing some imports, then build
   & test, then commit the changes, then push them to the central server;
   1. A *pre-commit* hook should detect if new imports were added that are not present in *_vendor* (or some imports removed which are
      present there) and stop the commit (or just inform, without stopping?); still, user should be allowed to commit the code anyway if he
      really wants (*"--force"*);
      1. **IMPLEMENTATION; VARIANT-A** (faster, but won't detect removed repos):
         1. analyze all `*.go` files changed by the commit (except `_*` etc.), including those in *_vendor* subdir; if they add any imports
            from outside main repo, which are not yet in *vendor.json*, then report **error** with appropriate message (list of pkgs and
            suggestion to call *vendo-add*);
      2. **IMPLEMENTATION; VARIANT-B** (slower, but will detect removed repos):
         1. `vendo-check-consistency` -- this checks that all repository roots listed in *vendor.json* exist as subdirs in the committed
            *_vendor* subdir, and that there are no other subdirs;
            1. `git stash -q --keep-index`;
            2. parse *vendor.json*, sort by pkg path;
            3. `os.Walk("_vendor", func...)`, where func...:
               1. if path is a prefix of pkg in *vendor.json*, then return CONTINUE;
               2. if path not in *vendor.json*, then report **error**, return SKIP\_SUBTREE;
               3. if has *.git/.hg/.bzr* subdir, then verify revision-id match with *vendor.json*; if failed, report **error** (see
                  *vendo-check-patched* for error message details);
               4. mark pkg visited;
               5. return SKIP\_SUBTREE;
            4. if any pkg in *vendor.json* is not visited, then report **error**;
            5. **TODO:** check that any *.git/.hg/.bzr* subdirs, if present, are at locations noted in $PKG_REPO_ROOT fields;
            6. `git stash pop -q`
         2. `vendo-check-dependencies` -- this checks that all packages imported by project are listed in the *vendor.json* file, and no
            others;
            1. `git stash -q --keep-index`; (or, work on files retrieved via git from index);
            2. iterate all \*.go files (except `_*` etc.), extract imports, and transitively their deps (same as in *vendo-add* - extract
               common code);
            3. delete from the list all pkgs in "core main repo" - i.e. those in main repo, but not in *_vendor*;
            4. verify that the list is *exactly* equal to contents of *vendor.json*; if not equal, report **error**;
            5. `git stash pop -q`;
         3. **TODO:** add a `vendo-check-json` step before 1. - it should verify internal consistency of *vendor.json* (pkg paths <->
            repository roots; same revision if same repositoryRoot; same revisionTime if same repositoryRoot);
   2. A tool must be available to auto-update (add & remove) packages in *_vendor* dir to satisfy the above *pre-commit* check; (still, we
      don't want to put the auto-update tool in *pre-commit* hook - we want user to run it explicitly, similar as with a *go fmt* hook);
   3. **IMPLEMENTATION**:
      1. `export GOPATH=$MAIN_REPO/_vendor:$GOPATH`
      2. work work work, edit some \*.go files; go get & go build & go test as needed;
      3. `git commit -a` -- if imports changed, this should fail because of *pre-commit* hook;
      4. `vendo-recreate`;
      5. `git commit -a` -- should complete successfully;
7. User wants to patch a repo in *_vendor* to fix a bug in a third-party repo;
   1. A *pre-commit* Git hook detects that changes were made in some packages, and require changing (adding or editing) the repo's
      `"comment"` field in the *vendor.json* file [a new revision-id would be desirable too, but it may not exist in the original repo, thus
      becoming nonsensical; also, old revision-id has advantage of keeping info about base commit; disadvantage is that *vendor.json* drops
      consistency with *_vendor* contents];
      1. **IMPLEMENTATION** - *"vendo-check-patched"* (*"vendo-check-status"*? *"changes"*?):
         1. `cd _vendor; git status`; if no changes, we're ok, exit early.
         2. find out which repos changed (by taking repo roots from *vendor.json*);
         3. for each changed repo:
            1. if has *.git/.hg/.bzr*, then:
               1. if current revision (via git/hg/bzr) differs from revision-id from *vendor.json*, report **error**;
                  * *[Note]* The error message should list all possible (known) cases and suggested solutions. Suggested message contents:
                        The revision in local repository at $PKG_REPO_ROOT:
                          $PKG_LOCAL_REVISION $PKG_LOCAL_REV_DATE $PKG_LOCAL_REV_COMMENT
                        is inconsistent with information stored in 'vendor.json' for package $PKG:
                          $PKG_REPO_REVISION $PKG_REPO_REV_DATE
                          comment: $PKG_JSON_COMMENT
                        To fix the inconsistency, you are advised do one of the following actions,
                        depending on which is most appropriate in your case:
                          a) revert $PKG_REPO_ROOT to $PKG_REPO_REVISION;
                          b) update "revision" in 'vendor.json' to $PKG_LOCAL_REVISION;
                          c) delete $PKG_REPO_ROOT/.git   [TODO: or .hg or .bzr]
                  * *[Note]* Possible (known) reasons for such situation:
                    * (a) user did `go get -u` without changing *vendor.json*;
                    * (b) user did a patch in the subrepo, then did `git commit` in the subrepo - that would be OK here, but on disk this
                      looks exactly the same as (a), so we cannot differentiate it;
                    * (c) user pulled main repo with updated pkg in *_vendor* (and *vendor.json*), while having old (pre-update) *.git* dir
                      in the pkg's (sub)repo; (see also case (d) below, as it's effectively the same);
                      * solution proposal: run *vendo-update* for the pkg, with a flag which will make it stop after the step:
                        *"6. if \`git/hg/bzr status [...]"*; (+ updating the "master"/other appropriate branch in *.git/.hg/.bzr*);
                    * (d) user checkouts from pre-update revision of main repo, to post-update one (and
                        reverse), while having the *.git/...* subdir; we can suggest deleting *.git/...*, or doing appropriate
                        *vendo-update* each time;
               2. `cd $PKG_REPO_ROOT; git/hg/... status`; if clean, go to next repo (this repo is ok);
               3. otherwise (not clean), *assume repo is "dirty"/"patched"* -- see points below;
            2. assume repo is "dirty"/"patched"; for this repo, check if `"comment"` in *vendor.json* is changed between pre-commit and
               post-commit; if not, report **error** with message asking user to change the "comment" in *vendor.json* (user should notify
               in the comment about the patch);
   2. *[Note]* The repo in *_vendor* may or may not have a *.git/.hg/.bzr* subdir;
   3. **IMPLEMENTATION**:
      1. [first time] set up a *pre-commit* hook as described above;
      2. edit files in a pkg in *_vendor* dir;
      3. try `git add _vendor/... ; git commit` -- it should fail, because of *pre-commit* hook, with appropriate message (`please edit
         "comment" in vendor.json for repo ... to mention that it was patched locally`);
      4. edit *vendor.json*: add/modify a `"comment"` field for the repo, so that it mentions the patch contents and maybe version, e.g.:
         `"comment": "PATCHED(v2) to fix a data race"`; (or maybe: `"comment": "PATCHED(2015-07-01) to fix a data race"`?)
      5. try `git add vendor.json ; git commit` -- now it should succeed;

This solution looks kinda costly to build now; but the main benefit it brings, is that the repo should become fully self-contained, and
especially all historic builds (since this solution is introduced) will be reproducible too, with correct versions of dependencies.
