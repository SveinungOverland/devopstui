# Playbook: implement an issue

You are the implementing agent. The issue has been moved to `Ready` on the
The issue has been labelled `agent:ready`, a branch and a draft pull request
already exist, and the issue now reads `agent:in-progress`. Your job is to
finish the work on that branch.

Read `.github/claude/CONTEXT.md` first — project, package layout, how to
verify, how to see the UI.

## Before writing code

1. Read the issue and **the plan comment on it**. The plan is the agreed
   approach; follow it. If the code turns out to contradict the plan, say so in
   the PR description and explain what you did instead — do not silently build
   something else.
2. Read any answers the human left in the issue comments, especially under
   "Decisions needed". A later comment from a human wins over the plan.
3. Look at the files the plan names before changing any of them.

## Building it

- Work only on the current branch. It is already checked out.
- Keep the change to what the issue asks for. If you find an unrelated bug,
  mention it in the PR description instead of fixing it here.
- Match the style of the file you are editing. `.github/claude/CONTEXT.md` has
  the conventions that matter in this repo.
- Add or extend a test for the behaviour you changed. For anything that touches
  `internal/ui`, use the harness in `internal/ui/app_test.go` — drive the keys a
  user would press and assert on the view or on `h.fake.Updates`.

## Verifying it

Before you push, all of this must pass:

```bash
make check     # go vet, go test ./..., gofmt
make build
```

Then look at what you built:

```bash
make shots                                    # the standard tour
scripts/tui-shot.sh --name <change> --keys "…"  # the specific thing you changed
```

Read the capture. If the layout is broken, the colours are wrong, or the keys
do not do what you expected, that is a failure even with green tests — fix it
and capture again. Check a narrow terminal too (`--size 80x24`) if you touched
layout.

## Finishing

1. Commit with a message that says what changed and why, in the style of the
   existing history (`git log`). Reference the issue.
2. Push to the current branch.
3. Update the pull request description with `gh pr edit`:

   ```markdown
   Closes #<issue>

   ## What changed
   Two or three sentences.

   ## How it looks
   ```text
   (paste the relevant shots/*.txt capture here — before and after if the
   change alters existing layout; skip this section only if nothing visible
   changed)
   ```

   ## Verification
   - `make check` — passing
   - the tests added or changed, by name

   ## Notes for the reviewer
   Anything you were unsure about, anything you left out, anything you noticed
   in passing. Be honest about what is not covered.
   ```

Do not mark the pull request ready for review and do not change the `agent:`
labels — the workflow does both once this run finishes cleanly.

If you cannot finish — the issue is underspecified, the approach in the plan
does not work, or something is genuinely broken — stop, push whatever is
coherent, and say exactly what blocked you in a PR comment. A clear "this needs
a decision" beats a guess that has to be unpicked later. Do not write `@claude`
in any comment.
