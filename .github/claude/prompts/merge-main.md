# Playbook: resolve a merge of the default branch

Someone labelled a pull request `agent:merge-main`. The workflow already tried
the plain merge, git could not finish it, and you have the conflicted worktree.
Your whole job is to produce one honest merge commit.

Read `.github/claude/CONTEXT.md` first — the package layout and the verification
commands are there.

## What you are resolving

A conflict is two people answering the same question differently. Find out what
each side was doing before you pick:

```bash
git log --oneline HEAD..MERGE_HEAD      # what the default branch added
git log --oneline MERGE_HEAD..HEAD      # what this pull request adds
git diff --name-only --diff-filter=U    # the files in dispute
gh pr view <pr>                         # what this pull request is for
```

For a single file, `git log --merge -p -- <file>` shows only the commits that
touched it on either side. That is usually enough to see the intent.

## Resolve

1. **Keep both intents.** The common case is not "theirs or mine": it is two
   changes that both belong in the result. A function renamed on the default
   branch and called in a new place on this branch means the new call gets the
   new name, not that one of them loses.

2. **Resolve to working code, not to a merge of the text.** Read the whole
   function after you edit it. A resolution that stitches both hunks together
   into something that never worked is worse than either side.

3. **Watch the files that are lists.** `internal/ui/keymap.go` is the single
   source of truth for key bindings, and two branches adding bindings conflict
   in a block where the answer is almost always "take both, in a sensible
   order". `go.mod`, `go.sum`, `PLAN.md` and `README.md` behave the same way.
   For `go.sum`, resolve `go.mod` by hand and then run `go mod tidy` rather than
   editing hashes.

4. **`git checkout --ours` / `--theirs` is a decision, not a shortcut.** Use it
   only when one side genuinely supersedes the other, and say which in the merge
   commit message.

5. **Do not use the merge to change anything else.** No cleanups, no
   refactoring, no fixing something you noticed in passing. A merge commit that
   also contains unrelated edits is unreviewable. Note what you noticed in your
   summary comment instead.

6. **Semantic conflicts do not show up as conflicts.** After the textual
   resolution, check whether the default branch changed something this branch
   calls — a signature, a field name, a constructor — in a file git merged
   without complaint. `make build` is what catches this.

## Finish

```bash
make check     # vet + test + gofmt. Must pass.
```

If the merged result needs a real code change to work — a call site updated to
a new signature, a test's expectation updated to match a change from the default
branch — make it. That is part of the merge.

If `make check` fails for a reason you cannot fix inside the merge, stop, leave
the conflicts resolved as far as you got, and explain it in a PR comment. A
wrong merge pushed quietly is much more expensive than a run that failed.

Then commit the merge, and only the merge:

```bash
git add <the files you resolved>
git commit            # keep the generated merge message, add a line per
                      # non-obvious resolution under it
```

Do not push, do not rebase or amend commits that were already on the branch, do
not touch the pull request's labels or draft state, and do not write `@claude`
in any comment. The workflow verifies, pushes and comments once you finish.

Finally, post one PR comment: a short list of what conflicted and how you
resolved each one, in a sentence each. Flag anything you were unsure about
explicitly — the human reading the merge commit needs to know where to look.
