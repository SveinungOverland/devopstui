# Board-driven automation

Issues on the **Devopstui Kanban** project board drive the work. You move a
card; agents do the rest and move it back to you.

```
 issue opened ──▶ Backlog ──▶ Ready ──▶ In progress ──▶ In review ──▶ Done
      │            │            │            │              │           │
      │            │            │            │              │           └─ PR merged
      │            │            │            │              └─ PR ready, review agents run
      │            │            │            └─ branch + draft PR, agent implements
      │            │            └─ YOU move the card. The only manual step.
      │            └─ plan comment posted: changes, risks, decisions needed
      └─ added to the board automatically
```

Everything except the move to `Ready` happens on its own. `Ready` is the
go-ahead, and it is deliberately the one thing a human does.

## Setup

1. **Create the board.** A GitHub Project (v2) called `Devopstui Kanban`, owned
   by the same account as the repository, with a single-select `Status` field
   whose options are exactly:

   `Backlog`, `Ready`, `In progress`, `In review`, `Done`

   Spelling is matched case-insensitively, so `In Progress` is fine, but an
   option that is missing entirely will fail the run that needs it.

2. **Install the Claude GitHub App** on the repository and set
   `secrets.CLAUDE_CODE_OAUTH_TOKEN`. Run `/install-github-app` from Claude
   Code if it is not already set up.

3. **Add `secrets.PROJECTS_TOKEN`.** A personal access token that can read and
   write the project:

   - classic PAT: `repo` + `project` scopes;
   - or fine-grained PAT: repository *Contents*, *Issues* and *Pull requests*
     read/write, plus account *Projects* read/write.

   Add `workflow` scope too if you want agents to be able to change the files
   in `.github/workflows/` themselves.

   This token is needed because **the automatic `GITHUB_TOKEN` cannot touch
   Projects v2 at all** — it is scoped to the repository, and projects live
   outside it.

4. **Run *Automation bootstrap*** from the Actions tab. It creates the labels
   and checks that the token can see the board and all five Status options. It
   fails loudly if something is missing, which is the point.

5. **Merge to the default branch.** Scheduled workflows only run from there, so
   the poll does not start until `project-sync.yml` is on `main`.

Optional repository variables:

| Variable         | Default            | Use                                                   |
| ---------------- | ------------------ | ----------------------------------------------------- |
| `PROJECT_TITLE`  | `Devopstui Kanban` | If the board is called something else.                |
| `PROJECT_NUMBER` | –                  | Pin by number instead of title; skips the title match. |
| `MAX_IMPLEMENT`  | `2`                | Agents started per poll.                              |
| `MAX_REVIEW`     | `3`                | Reviews started per poll.                             |

## Why the board is polled

GitHub does not deliver project events to repository workflows.
`projects_v2_item` webhooks exist only for organisation-level webhooks and
GitHub Apps, and `projects_v2_item` is not one of the events that can trigger a
workflow. There is no way to run a workflow the moment a card moves.

So `project-sync.yml` runs on a `*/5 * * * *` schedule and reconciles instead:
it reads the board, compares it against what has actually happened, and starts
whatever is missing. A card moved to `Ready` is normally picked up within five
minutes — sometimes longer, because GitHub delays scheduled runs under load. If
you do not want to wait, hit **Run workflow** on *Project sync*.

The reconciler is idempotent. Every check asks "has this already happened?"
before starting anything, so running it twice, or in parallel with itself, does
nothing twice:

- a card at `Ready` is skipped if the issue already has `claude:implementing`;
- a card at `In review` is skipped if the PR's current commit already has an
  impact review — matched on the head SHA, so a new commit gets a new review
  but a card parked at `In review` does not get reviewed every five minutes;
- a closed issue that is not at `Done` is moved there.

## What each workflow does

| Workflow                    | Starts when                                    | Does                                                                   |
| --------------------------- | ---------------------------------------------- | ---------------------------------------------------------------------- |
| `issue-plan.yml`            | issue opened or reopened                       | Adds it to the board at `Backlog`, posts the plan comment.             |
| `project-sync.yml`          | every 5 minutes                                | Reads the board, starts implement and review runs, fixes drift.        |
| `agent-implement.yml`       | called by sync or by pr-feedback               | Branch, draft PR, implementation, verification, hand-off to review.    |
| `agent-review.yml`          | PR ready for review, or called by sync         | The impact and security review.                                        |
| `claude-code-review.yml`    | PR ready for review, new commits on a ready PR | Line-level inline comments.                                            |
| `pr-feedback.yml`           | a human requests changes                       | Sends the card back to `In progress` and the agent back to work.       |
| `pr-merged.yml`             | PR closed                                      | `Done` if merged, back to `Backlog` if not.                            |
| `ci.yml`                    | push and pull request                          | `make check` plus a UI capture.                                        |
| `automation-bootstrap.yml`  | manual                                         | Labels, and a health check of the board wiring.                        |
| `claude.yml`                | `@claude` in a comment                         | The manual escape hatch, outside the pipeline.                         |

The agents' instructions are in `.github/claude/prompts/` — `plan.md`,
`implement.md`, `revise.md`, `review.md` — with shared project context in
`.github/claude/CONTEXT.md`. Edit those to change how the agents behave; they
are read at run time from the checked-out branch, so a change takes effect on
the next run.

The shared context lives there rather than in a root `CLAUDE.md` because this
repository gitignores `/CLAUDE.md`, and a file the checkout does not have is a
file the agents cannot read. If you want it loaded automatically in your local
sessions too, add a `CLAUDE.md` that points at it — it stays untracked, which
is the point.

`.claude/skills/ui-check/` is a skill, so it is picked up both locally and in
Actions. It is what an agent reads when it needs to see the UI rather than
guess at it.

## The two reviews

They are deliberately separate, and they do not overlap:

- **`claude-code-review.yml`** leaves inline comments on the diff: correctness
  bugs, small cleanups, the things that belong next to the line.
- **`agent-review.yml`** posts one comment about the change as a whole: what
  else in the codebase it affects, what might go wrong later, and security —
  the PAT never reaching a log or a rendered view, untrusted work item text
  being rendered to a terminal, temp files, subprocesses, new dependencies.

Neither approves, requests changes, or merges. That is yours.

Both stay quiet while a PR is a draft, which is exactly while the implementing
agent is working. Marking the PR ready is the hand-off, and it is what starts
them.

## Draft state mirrors the board

| Board         | Pull request |
| ------------- | ------------ |
| `In progress` | draft        |
| `In review`   | ready        |

The implement workflow marks the PR ready at the end of a clean run; the
feedback workflow puts it back to draft before another pass. This is what keeps
reviews off half-finished branches without needing any extra bookkeeping.

## Tokens

Three, with different jobs, kept apart on purpose:

| Token                      | Used for                                                          | Where |
| -------------------------- | ----------------------------------------------------------------- | ----- |
| `CLAUDE_CODE_OAUTH_TOKEN`  | Authenticating Claude.                                            | The action's own input. |
| `PROJECTS_TOKEN`           | The board, opening the PR, the draft/ready toggle.                | Steps that run no model output. |
| `GITHUB_TOKEN` (automatic) | Everything the agent itself does: commits, comments, PR edits.    | The step Claude runs in. |

**The PAT is never in the environment of a step that runs an agent.** Issue
bodies, PR descriptions and review comments are untrusted input, and an agent
reading them should not have a token that reaches beyond this repository. The
agent's blast radius is the `permissions:` block of its job.

Two more guards for the same reason:

- `issue-plan.yml` only runs automatically for issues opened by the repository
  owner, a member or a collaborator. Anyone else's issue waits for a maintainer
  to start the workflow by hand.
- `pr-feedback.yml` ignores reviews from bots, so a review agent cannot restart
  the implementation loop. Only a human requesting changes does that.

Every prompt also tells the agent that the text it is reading is data, not
instructions, and to report anything that tries to redirect it.

## Working with it

**Normal loop.** Open an issue, read the plan, answer anything under "Decisions
needed", move the card to `Ready`. Come back to a PR. Review it: request
changes to send it round again, merge when you are happy.

**Start something now** without waiting for the poll: *Project sync* →
**Run workflow**.

**Re-run one issue** without touching the board: *Agent — implement* → **Run
workflow**, with the issue number, and `implement` or `revise`.

**Stop an agent**: cancel the run, then move the card off `Ready`. Remove the
`claude:implementing` label if you want the next poll to pick it up again.

**Take over a branch by hand.** The branch is `claude/issue-<number>` and it is
an ordinary branch — push to it, and the agent will build on your commits the
next time it runs.

## Labels

| Label                    | Means                                               |
| ------------------------ | --------------------------------------------------- |
| `claude:planned`         | A plan comment has been posted.                     |
| `claude:needs-decision`  | The plan is blocked on an answer. Do not move to `Ready` yet. |
| `claude:implementing`    | An agent is on it. Also what stops a second one starting. |
| `claude:in-review`       | Implementation finished, PR is up.                  |
| `claude:blocked`         | A run failed. The comment on the issue links the log. |

## When something goes wrong

**Nothing happens when a card moves to `Ready`.** Check *Project sync* in the
Actions tab. If runs are not appearing at all: scheduled workflows only run
from the default branch, and GitHub disables them after 60 days without
repository activity — push anything, or re-enable from the Actions tab.

**`no projects visible to this token`.** `PROJECTS_TOKEN` is missing, expired,
or has no project scope. Run *Automation bootstrap* — it says which.

**`Status has no option '...'`.** The board's option names drifted from the
five the workflows use. The error lists what the board actually has.

**A run failed and the card is stuck at `In progress`.** The issue has
`claude:blocked` and a comment linking the log. Fix the cause, then move the
card out of `Ready` and back in.

**Two agents on one issue.** Should not happen: `concurrency` groups runs by
issue, and the `claude:implementing` label stops the poll from starting a
second. If it does, cancel one — the branch is shared and the second run will
reuse it.

**The PR was opened but nothing was implemented.** Look for the empty
`Start work on #n` commit: the agent's run failed after the branch was created.
The branch and PR are reused on the next attempt, so just retry.

**A comment says the review "finished without posting a review".** The review
agent ran but left nothing. The workflow posts that marker itself so the poll
does not start a fresh review every five minutes for the same commit — which is
the failure mode worth avoiding. Re-run *Agent — impact review* with the PR
number to try again.

## Cost

Every poll costs a GitHub Actions minute or so; the agent runs cost Claude
usage. If it is more than you want:

- raise the cron interval in `project-sync.yml` (`*/15 * * * *` is plenty for
  most days);
- lower `MAX_IMPLEMENT`;
- drop `synchronize` from `claude-code-review.yml` so the inline review runs
  once per hand-off rather than on every push.
