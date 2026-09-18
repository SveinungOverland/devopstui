# Playbook: address review feedback

A human sent the pull request back — either by requesting changes on it or by
converting it to a draft. The issue is back at `agent:in-progress` and the PR is
a draft again. Your job is to address the feedback on the existing branch.

There may be no formal review to read: the person who opens these pull requests
cannot request changes on their own, so their feedback arrives as ordinary PR
comments, inline comments, or a comment on the issue. Gather it from all of
those before you start.

Read `.github/claude/CONTEXT.md` first, then the playbook rules in
`.github/claude/prompts/implement.md` for building, verifying and showing the
UI — they apply here too.

## Work

1. Read **every** unresolved comment on the PR, not just the summary, and the
   issue thread too:

   ```bash
   gh pr view <pr> --comments
   gh api repos/{owner}/{repo}/pulls/<pr>/comments --paginate
   gh issue view <issue> --comments
   ```

   If you genuinely find no feedback anywhere, say so in a PR comment and stop
   rather than guessing at changes nobody asked for.

2. Handle each one. There are only three honest outcomes per comment:
   - **Fix it.** The usual case. Change the code.
   - **Fix it differently.** If the suggestion has a problem, do the thing that
     solves the reviewer's actual concern, and reply on that thread saying what
     you did instead and why.
   - **Push back.** If the suggestion is wrong or would break something, reply
     on the thread with the reason. Do not quietly skip it.

   Reply on the thread the comment is on, so the conversation stays where the
   reviewer left it.

3. A review comment about one line often applies in several places. Fix the
   pattern, not just the line that was pointed at.

## Finishing

- `make check` and `make build` must pass.
- Re-capture the UI if the feedback changed anything visible, and update the
  "How it looks" section of the PR description.
- Commit, push to the same branch.
- Post one comment summarising what changed this round, as a short list keyed
  to the feedback. No essay.

Do not mark the PR ready for review and do not change the `agent:` labels — the
workflow does both when this run finishes cleanly. Do not write `@claude` in any comment.

Never resolve a human's review thread yourself unless you fixed exactly what it
asked for; leaving it open is how the reviewer knows to look again.
