#!/usr/bin/env python3
# Licensed to Julian Hyde under one or more contributor license
# agreements.  See the NOTICE file distributed with this work
# for additional information regarding copyright ownership.
# Julian Hyde licenses this file to you under the Apache
# License, Version 2.0 (the "License"); you may not use this
# file except in compliance with the License.  You may obtain a
# copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
# either express or implied.  See the License for the specific
# language governing permissions and limitations under the
# License.
"""Gate and dashboard for convergence of morel-go's script test
files toward morel-java's.

Gate mode (the default) checks one commit: for every shared
script file it compares the number of lines differing from
morel-java BEFORE the commit (at the parents) and AFTER. It fails
if any file became MORE divergent — a section that java changed
but go did not follow, even if other files converged enough to
hide it in the net. The java commit is read from the go commit's
`Propagates ... commit <sha>` line. A commit without one is refused:
it used to be compared against whatever the java checkout was at,
which made the answer depend on when the gate ran rather than on what
the commit did.

Report mode (--report) is the project dashboard: per-file
divergence of the working tree against the java checkout's
working tree, java files not yet pulled, and go-only files.

Ledger mode (--ledger) lists the java commits already propagated,
read from `Propagates` lines in the git log.

Usage:
    etc/check-convergence.py --java-repo PATH [GO_COMMIT]
                             [--java JAVA_SHA] [--verbose]
    etc/check-convergence.py --java-repo PATH --report
                             [--go-rev REV] [--java-rev REV]
    etc/check-convergence.py --ledger

`--java-repo` is required and has no default: see the comment on it
below. Nothing else about the measurement is implicit either -- the
gate compares GO_COMMIT against its parent, and the morel-java commit
named in GO_COMMIT's `Propagates` line against *its* parent, so the
answer depends on the two commits and not on where either repository
happens to be checked out. Running the gate on an old commit today
gives what it gave when that commit was made.

`--report` is the exception, and is a dashboard rather than a gate: by
default it compares working trees, which is a snapshot. Name
`--go-rev` and `--java-rev` when the number has to mean the same thing
twice.
"""
import argparse
import difflib
import os
import re
import subprocess
import sys

GO_PREFIX = "testdata/script/"
JAVA_PREFIX = "src/test/resources/script/"
# There is deliberately no default morel-java checkout. A hardcoded
# path is not a criterion, it is a guess about which clone is current,
# and the two ports guessed differently -- morel-go at ~/dev/morel.1,
# morel-rust at ~/dev/morel.0 -- so the same propagation could pass
# one gate and fail the other. Pass --java-repo; any clone containing
# the commit being measured against gives the same answer, because the
# commits, not the path, are what the comparison is pinned to.

# Divergences that are settled, and why. A file listed here may differ
# by up to the given number of lines without failing the gate; beyond
# that it fails again, so the cap has to be raised deliberately. Use
# this only where morel-go cannot follow morel-java rather than has
# not: a dataset morel-go does not have, output morel-go deliberately
# omits, or a difference that is not a difference in meaning.
ACCEPTED = {
    "blog.smli": (
        288,
        "the `foodmart` dataset, which morel-go does not have. Its "
        "schema is dumped in full, so every morel-java change to a "
        "foodmart column type grows a block morel-go has no way to "
        "follow.",
    ),
    "foreign.smli": (
        186,
        "the `foodmart` dataset, as in blog.smli.",
    ),
    "hybrid.smli": (
        187,
        "the Calcite plans that `Sys.plan` prints. morel-go has no "
        "Calcite, so it omits every one of this file's plan "
        "assertions.",
    ),
}


def git(repo, *args):
    """Runs git in `repo`, returning stdout (None on failure)."""
    r = subprocess.run(
        ["git", "-C", repo, *args],
        capture_output=True,
        text=True,
    )
    return r.stdout if r.returncode == 0 else None


# The script corpus, by extension. A `.smli` script carries its own
# expected output and reproduces itself; a `.sml` script's transcript
# is the companion `.sml.out`, and both halves count, since a
# divergence may live in either. `.smli` does not end in `.sml`, so
# the suffixes do not overlap.
SCRIPT_SUFFIXES = (".smli", ".sml", ".sml.out")


def script_files(repo, commit, prefix):
    """Relative script paths (below `prefix`) present at `commit`,
    or in the working tree if `commit` is None."""
    if commit is None:
        files = set()
        top = os.path.join(repo, prefix)
        for dirpath, _dirs, names in os.walk(top):
            for name in names:
                if name.endswith(SCRIPT_SUFFIXES):
                    path = os.path.join(dirpath, name)
                    files.add(os.path.relpath(path, top))
        return files
    out = git(repo, "ls-tree", "-r", "--name-only", commit)
    if out is None:
        sys.exit(f"error: cannot list files at {commit} in {repo}")
    files = set()
    for line in out.splitlines():
        if line.startswith(prefix) and line.endswith(SCRIPT_SUFFIXES):
            files.add(line[len(prefix):])
    return files


def file_at(repo, commit, prefix, rel):
    """Content of `prefix+rel` at `commit` (working tree if
    `commit` is None), or empty if absent."""
    if commit is None:
        try:
            with open(os.path.join(repo, prefix, rel)) as f:
                return f.read()
        except OSError:
            return ""
    out = git(repo, "show", f"{commit}:{prefix}{rel}")
    return out if out is not None else ""


def diff_lines(a, b):
    """Number of differing lines between two file contents (added
    + removed, ignoring the unified-diff header lines)."""
    a_lines = a.splitlines(keepends=True)
    b_lines = b.splitlines(keepends=True)
    n = 0
    for line in difflib.unified_diff(a_lines, b_lines, n=0):
        if line.startswith(("+++", "---", "@@")):
            continue
        if line.startswith(("+", "-")):
            n += 1
    return n


def java_sha_from_message(repo, commit):
    """Extracts the morel-java SHA from `commit`'s 'Propagates ...
    commit' line."""
    msg = git(repo, "log", "-1", "--format=%B", commit) or ""
    m = re.search(
        r"[Pp]ropagates\s+\S*\s*commit\s+([0-9a-f]{7,40})", msg)
    return m.group(1) if m else None


def java_sha_for(repo, commit):
    """The morel-java commit to measure `commit` against, and whether it
    came from an ancestor.

    A propagation names its morel-java commit, and is measured against
    that commit and its parent: morel-java changed something, and the
    question is whether this commit followed.

    A commit of the port's own -- a refactor, a bug fix, anything with
    no `Propagates` line -- is measured against a single morel-java
    commit, the one the nearest ancestor propagated. That SHA is fixed
    in this repository's history, so the answer does not depend on when
    the gate is run; and because morel-java does not move across the
    comparison, any script that grows more divergent grew that way
    here.
    """
    sha = java_sha_from_message(repo, commit)
    if sha:
        return sha, False
    log = git(repo, "log", "--format=%H", f"{commit}~1") or ""
    for line in log.split("\n"):
        if not line.strip():
            continue
        sha = java_sha_from_message(repo, line.strip())
        if sha:
            return sha, True
    return None, False


def report(go_repo, java_repo, go_rev, java_rev):
    """Prints the dashboard: `go_rev` against `java_rev`.

    Either may be None, meaning that repository's working tree. A
    dashboard of two working trees is the useful default -- it says
    where the ports stand right now -- but it is a snapshot, not a
    reproducible measurement, so name revisions when the number has
    to mean the same thing twice."""
    go_files = script_files(go_repo, go_rev, GO_PREFIX)
    java_files = script_files(java_repo, java_rev, JAVA_PREFIX)

    shared = sorted(go_files & java_files)
    missing = sorted(java_files - go_files)
    extra = sorted(go_files - java_files)

    net = 0
    if shared:
        print(f"{'shared file':40} {'go':>6} {'java':>6} "
              f"{'diff':>6}")
        for rel in shared:
            a = file_at(go_repo, go_rev, GO_PREFIX, rel)
            b = file_at(java_repo, java_rev, JAVA_PREFIX, rel)
            d = diff_lines(a, b)
            net += d
            print(f"{rel:40} {len(a.splitlines()):6} "
                  f"{len(b.splitlines()):6} {d:6}")
        print()
    if missing:
        total = 0
        print("not yet pulled from morel-java:")
        for rel in missing:
            b = file_at(java_repo, java_rev, JAVA_PREFIX, rel)
            total += len(b.splitlines())
            print(f"  {rel:40} {len(b.splitlines()):6} lines")
        print(f"  ({len(missing)} files, {total} lines)")
        print()
    if extra:
        print("go-only (temporary scaffolding, to be deleted or "
              "upstreamed):")
        for rel in extra:
            print(f"  {rel}")
        print()
    print(f"net divergence of shared files: {net} lines; "
          f"{len(shared)} shared, {len(missing)} unpulled, "
          f"{len(extra)} go-only")
    return 0


def ledger(go_repo):
    """Prints propagated java commits, oldest first."""
    out = git(go_repo, "log", "--reverse", "--format=%H%x00%B%x01")
    if out is None:
        sys.exit("error: cannot read git log")
    rows = []
    for entry in out.split("\x01"):
        if "\x00" not in entry:
            continue
        sha, body = entry.split("\x00", 1)
        m = re.search(
            r"[Pp]ropagates\s+(\S*)\s*commit\s+([0-9a-f]{7,40})",
            body)
        if m:
            rows.append((sha.strip()[:9], m.group(2)[:9],
                         m.group(1)))
    for go_sha, java_sha, issue in rows:
        print(f"{go_sha}  propagates  {java_sha}  {issue}")
    print(f"({len(rows)} propagation commits)")
    return 0


def gate(go_repo, args):
    """Checks that one commit did not diverge from morel-java."""
    go = git(go_repo, "rev-parse", args.go_commit)
    if go is None:
        sys.exit(f"error: bad go commit {args.go_commit}")
    go = go.strip()
    go_parent = git(go_repo, "rev-parse", f"{go}^").strip()

    local = False
    if args.java:
        java = args.java
    else:
        java, local = java_sha_for(go_repo, go)
    if not java:
        sys.exit(
            f"error: {go[:9]} has no 'Propagates ... commit <sha>' line "
            f"and no ancestor has one either, so there is no java commit "
            f"to measure against.\n"
            f"       Pass --java <sha> to name one explicitly.")
    java_full = git(args.java_repo, "rev-parse", java)
    if java_full is None:
        sys.exit(f"error: bad java commit {java} in "
                 f"{args.java_repo}")
    java = java_full.strip()
    if local:
        # morel-java is held still, so the only thing that can move the
        # numbers is this commit.
        java_parent = java
    else:
        java_parent = git(args.java_repo, "rev-parse",
                          f"{java}^").strip()

    print(f"go    {go[:9]}  (parent {go_parent[:9]})")
    if local:
        print(f"java  {java[:9]}  (held still: this commit propagates "
              f"nothing, so it is measured against what its nearest "
              f"ancestor did)")
    else:
        print(f"java  {java[:9]}  (parent {java_parent[:9]})")
    print()

    rels = (
        script_files(go_repo, go, GO_PREFIX)
        | script_files(go_repo, go_parent, GO_PREFIX)
        | script_files(args.java_repo, java, JAVA_PREFIX)
        | script_files(args.java_repo, java_parent, JAVA_PREFIX)
    )

    regressions = []
    improvements = []
    accepted = []
    net_before = net_after = 0
    rows = []
    for rel in sorted(rels):
        before = diff_lines(
            file_at(go_repo, go_parent, GO_PREFIX, rel),
            file_at(args.java_repo, java_parent, JAVA_PREFIX,
                    rel),
        )
        after = diff_lines(
            file_at(go_repo, go, GO_PREFIX, rel),
            file_at(args.java_repo, java, JAVA_PREFIX, rel),
        )
        net_before += before
        net_after += after
        if after > before:
            if not file_at(args.java_repo, java, JAVA_PREFIX,
                           rel):
                # A go-only file (backswing.smli, parse-tree.smli) may
                # grow by design; it is scaffolding, not
                # divergence from java.
                rows.append((rel, before, after))
                continue
            if rel in ACCEPTED and after <= ACCEPTED[rel][0]:
                accepted.append((rel, after))
            else:
                regressions.append((rel, before, after))
        elif after < before:
            improvements.append((rel, before, after))
        rows.append((rel, before, after))

    if args.verbose:
        print(f"{'file':40} {'before':>7} {'after':>7} "
              f"{'delta':>7}")
        for rel, before, after in rows:
            if before == after:
                continue
            print(f"{rel:40} {before:7} {after:7} "
                  f"{after - before:+7}")
        print()

    if improvements:
        fewer = sum(b - a for _, b, a in improvements)
        print(f"converged: {len(improvements)} file(s), "
              f"{fewer} fewer differing lines")
    print(f"net divergence: {net_before} -> {net_after} "
          f"({net_after - net_before:+d} lines)")
    print()

    for rel, after in accepted:
        limit, why = ACCEPTED[rel]
        print(f"accepted divergence: {rel} ({after} of {limit} lines)")
        print(f"  {why}")
    if accepted:
        print()

    if regressions:
        print(f"FAIL: {len(regressions)} file(s) diverged further "
              f"from morel-java:")
        for rel, before, after in regressions:
            why = ("this commit diverged it" if local
                   else "java changed this; go did not follow")
            print(f"  {rel:40} {before:7} -> {after:7} "
                  f"({after - before:+d})  -- {why}")
        return 1

    print("OK: no script file diverged further from morel-java.")
    return 0


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("go_commit", nargs="?", default="HEAD")
    p.add_argument("--java",
                   help="morel-java commit SHA (else from message)")
    p.add_argument("--java-repo", required=True,
                   help="path to a morel-java clone that contains the "
                        "commit being measured against")
    p.add_argument("--go-rev",
                   help="revision of this repo for --report "
                        "(default: the working tree)")
    p.add_argument("--java-rev",
                   help="revision of the morel-java repo for --report "
                        "(default: its working tree)")
    p.add_argument("--report", action="store_true",
                   help="dashboard: working tree vs java checkout")
    p.add_argument("--ledger", action="store_true",
                   help="list propagated java commits")
    p.add_argument("--verbose", action="store_true",
                   help="list every file, not just regressions")
    args = p.parse_args()

    go_repo = os.getcwd()
    if args.report:
        return report(go_repo, args.java_repo,
                      args.go_rev, args.java_rev)
    if args.ledger:
        return ledger(go_repo)
    return gate(go_repo, args)


if __name__ == "__main__":
    sys.exit(main())
