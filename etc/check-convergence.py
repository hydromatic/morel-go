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
"""Gate and dashboard for convergence of morel-go's `.smli` test
files toward morel-java's.

Gate mode (the default) checks one commit: for every shared
`.smli` file it compares the number of lines differing from
morel-java BEFORE the commit (at the parents) and AFTER. It fails
if any file became MORE divergent — a section that java changed
but go did not follow, even if other files converged enough to
hide it in the net. The java commit is read from the go commit's
`Propagates ... commit <sha>` line; a commit without one (e.g.
corpus growth during bootstrap) is compared against the same java
commit on both sides, so only go's own change is measured.

Report mode (--report) is the project dashboard: per-file
divergence of the working tree against the java checkout's
working tree, java files not yet pulled, and go-only files.

Ledger mode (--ledger) lists the java commits already propagated,
read from `Propagates` lines in the git log.

Usage:
    etc/check-convergence.py [GO_COMMIT] [--java JAVA_SHA]
                             [--java-repo PATH] [--verbose]
    etc/check-convergence.py --report [--java-repo PATH]
    etc/check-convergence.py --ledger
"""
import argparse
import difflib
import os
import re
import subprocess
import sys

GO_PREFIX = "testdata/script/"
JAVA_PREFIX = "src/test/resources/script/"
DEFAULT_JAVA_REPO = os.path.expanduser("~/dev/morel.0")


def git(repo, *args):
    """Runs git in `repo`, returning stdout (None on failure)."""
    r = subprocess.run(
        ["git", "-C", repo, *args],
        capture_output=True,
        text=True,
    )
    return r.stdout if r.returncode == 0 else None


def smli_files(repo, commit, prefix):
    """Relative `.smli` paths (below `prefix`) present at
    `commit`, or in the working tree if `commit` is None."""
    if commit is None:
        files = set()
        top = os.path.join(repo, prefix)
        for dirpath, _dirs, names in os.walk(top):
            for name in names:
                if name.endswith(".smli"):
                    path = os.path.join(dirpath, name)
                    files.add(os.path.relpath(path, top))
        return files
    out = git(repo, "ls-tree", "-r", "--name-only", commit)
    if out is None:
        sys.exit(f"error: cannot list files at {commit} in {repo}")
    files = set()
    for line in out.splitlines():
        if line.startswith(prefix) and line.endswith(".smli"):
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


def report(go_repo, java_repo):
    """Prints the dashboard: working tree vs working tree."""
    go_files = smli_files(go_repo, None, GO_PREFIX)
    java_files = smli_files(java_repo, None, JAVA_PREFIX)

    shared = sorted(go_files & java_files)
    missing = sorted(java_files - go_files)
    extra = sorted(go_files - java_files)

    net = 0
    if shared:
        print(f"{'shared file':40} {'go':>6} {'java':>6} "
              f"{'diff':>6}")
        for rel in shared:
            a = file_at(go_repo, None, GO_PREFIX, rel)
            b = file_at(java_repo, None, JAVA_PREFIX, rel)
            d = diff_lines(a, b)
            net += d
            print(f"{rel:40} {len(a.splitlines()):6} "
                  f"{len(b.splitlines()):6} {d:6}")
        print()
    if missing:
        total = 0
        print("not yet pulled from morel-java:")
        for rel in missing:
            b = file_at(java_repo, None, JAVA_PREFIX, rel)
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

    java = args.java or java_sha_from_message(go_repo, go)
    if java:
        java_full = git(args.java_repo, "rev-parse", java)
        if java_full is None:
            sys.exit(f"error: bad java commit {java} in "
                     f"{args.java_repo}")
        java = java_full.strip()
        java_parent = git(args.java_repo, "rev-parse",
                          f"{java}^").strip()
    else:
        # Not a propagation: measure go's own change against one
        # fixed java commit.
        java = git(args.java_repo, "rev-parse", "HEAD").strip()
        java_parent = java

    print(f"go    {go[:9]}  (parent {go_parent[:9]})")
    print(f"java  {java[:9]}  (parent {java_parent[:9]})")
    print()

    rels = (
        smli_files(go_repo, go, GO_PREFIX)
        | smli_files(go_repo, go_parent, GO_PREFIX)
        | smli_files(args.java_repo, java, JAVA_PREFIX)
        | smli_files(args.java_repo, java_parent, JAVA_PREFIX)
    )

    regressions = []
    improvements = []
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

    if regressions:
        print(f"FAIL: {len(regressions)} file(s) diverged further "
              f"from morel-java:")
        for rel, before, after in regressions:
            print(f"  {rel:40} {before:7} -> {after:7} "
                  f"({after - before:+d})  -- java changed this; "
                  f"go did not follow")
        return 1

    print("OK: no .smli file diverged further from morel-java.")
    return 0


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("go_commit", nargs="?", default="HEAD")
    p.add_argument("--java",
                   help="morel-java commit SHA (else from message)")
    p.add_argument("--java-repo", default=DEFAULT_JAVA_REPO)
    p.add_argument("--report", action="store_true",
                   help="dashboard: working tree vs java checkout")
    p.add_argument("--ledger", action="store_true",
                   help="list propagated java commits")
    p.add_argument("--verbose", action="store_true",
                   help="list every file, not just regressions")
    args = p.parse_args()

    go_repo = os.getcwd()
    if args.report:
        return report(go_repo, args.java_repo)
    if args.ledger:
        return ledger(go_repo)
    return gate(go_repo, args)


if __name__ == "__main__":
    sys.exit(main())
