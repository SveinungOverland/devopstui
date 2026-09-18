# Label-driven automation

Labels on an issue drive the work. You add one label; agents do the rest and
hand it back to you.

```
 issue opened ──▶ backlog ──▶ agent:ready ──▶ agent:in-progress ──▶ agent:in-review ──▶ closed
      │             │             │                  │                    │               │
      │             │             │                  │                    │               └─ PR merged
      │             │             │                  │                    └─ PR ready, reviews run
      │             │             │                  └─ branch + draft PR, agent implements
      │             │             └─ YOU add this label. The only manual step.
      │             └─ plan comment posted: changes, risks, decisions needed
      └─ an open issue with no agent: label is the backlog
```

Adding `agent:ready` is the only manual step. Everything else is written by the
workflows.

## Setup

1. **Install the Claude GitHub App** on the repository and set
   `secrets.CLAUDE_CODE_OAUTH_TOKEN`. Run `/install-github-app` from Claude
   Code if it is not already set up.

2. **Run *Automation bootstrap*** from the Actions tab. It creates the six
   `agent:` labels so `agent:ready` is there to pick from the label menu.

3. **Merge to the default branch.** `claude-code-action` refuses to run from a
   workflow file whose content differs from the version on the default branch.
   On a pull request that adds or edits one you will see:

   > Workflow validation failed. The workflow file must exist and have
   > identical content to the version on the repository's default branch.

   That is the action protecting itself, not a misconfiguration. It also means
   an agent's own pull request that edits a file under `.github/workflows/`
   will have its Claude reviews skipped on that PR. The prompt playbooks under
   `.github/claude/` are ordinary files, not workflows, so those take effect on
   the branch immediately.

That is the whole required setup. There is no project, no board, and **no
personal access token** — labels are part of the repository, so the automatic
`GITHUB_TOKEN` can read and write them.

### The one optional extra

`secrets.AUTOMATION_TOKEN`, a PAT with `repo` scope, changes exactly one thing:
whether agent pull requests show CI check badges.

Events raised with the automatic `GITHUB_TOKEN` deliberately do not start
further workflows — that is GitHub preventing infinite loops. So a PR opened
and a branch pushed by an agent raise no `pull_request` events, and `ci.yml`
never runs on them. Set `AUTOMATION_TOKEN` and the PR is opened and marked
ready with it instead, which does raise events, and the checks appear.

Without it nothing breaks:

- `agent-implement.yml` runs `make check` itself before handing the PR over,
  and refuses to advance the issue if it fails;
- the impact review runs as a chained job rather than off a `ready_for_review`
  event.

## Why labels rather than a project board

A project board was the obvious fit, and it does not work: GitHub will not
start a repository workflow when a project card moves. `projects_v2_item`
webhooks exist only at organisation level and are not an Actions trigger, and
Projects v2 is outside the reach of `GITHUB_TOKEN` entirely, so a board version
of this needs a scheduled poll, a PAT with project scope, and a reconciler to
compare board state against reality.

Labels have none of those problems:

- `issues: labeled` is a first-class workflow trigger, so work starts the
  moment you add the label rather than up to five minutes later;
- labels live in the repository, so `GITHUB_TOKEN` is enough;
- **only someone with write access can add a label**, so the permission check
  on the trigger is GitHub's rather than something this repository has to
  implement.

The last one matters more than it looks. `agent:ready` is the point where an
agent starts writing code, and it is gated by repository permissions without a
line of code on our side.

## What each workflow does

| Workflow                   | Starts when                                    | Does                                                                |
| -------------------------- | ---------------------------------------------- | ------------------------------------------------------------------- |
| `issue-plan.yml`           | issue opened or reopened                       | Posts the plan comment, ensures the labels exist. Fails if no plan lands. |
| `agent-implement.yml`      | `agent:ready` added                            | Branch, draft PR, implementation, verification, hand-off to review. |
| `agent-review.yml`         | chained from implement, or PR ready for review | The impact and security review.                                     |
| `claude-code-review.yml`   | PR ready for review, new commits on a ready PR | Line-level inline comments.                                         |
| `pr-feedback.yml`          | a human requests changes                       | Puts the agent back on it, in `revise` mode.                        |
| `pr-merged.yml`            | PR closed                                      | Closes the issue if merged, clears the labels either way.           |
| `ci.yml`                   | push and pull request                          | `make check` plus a UI capture.                                     |
| `automation-bootstrap.yml` | manual                                         | Creates the labels.                                                 |
| `claude.yml`               | `@claude` in a comment                         | The manual escape hatch, outside the pipeline.                      |

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

## A run is judged on what it produced

An agent exiting without an error does not mean it did the work, so no workflow
takes the run's own word for it. Each one checks for its artefact afterwards:

- `issue-plan.yml` looks for the plan comment, which the agent marks with
  `<!-- claude-issue-plan: <issue> -->`. Found, the issue gets `agent:planned` —
  the label means a plan exists, so it is applied on one existing. Not found,
  the issue gets `agent:blocked` and a comment saying nothing was produced, and
  the run fails rather than finishing green over an empty issue.
- `agent-implement.yml` runs `make check` against the branch, and pushes
  anything the agent committed but did not push.
- `agent-review.yml` looks for its own `claude-impact-review: <sha>` marker and
  notes on the PR when a review produced nothing.

So a green run means the artefact is there, and a red one is worth opening.

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
agent is working. Marking the PR ready is the hand-off.

The impact review can be asked for twice — once by the chained job and once by
`ready_for_review` when `AUTOMATION_TOKEN` is set. The first one to finish
leaves a `<!-- claude-impact-review: <sha> -->` marker, and the second sees it
and stands down. A review that produced nothing does *not* leave a marker, so
the other path is still free to try.

## Draft state mirrors the label

| Issue               | Pull request |
| ------------------- | ------------ |
| `agent:in-progress` | draft        |
| `agent:in-review`   | ready        |

The implement workflow marks the PR ready at the end of a clean run; the
feedback workflow puts it back to draft before another pass. This is what keeps
reviews off half-finished branches without extra bookkeeping.

## Tokens

| Token                      | Used for                                                       |
| -------------------------- | --------------------------------------------------------------- |
| `CLAUDE_CODE_OAUTH_TOKEN`  | Authenticating Claude. Required.                               |
| `GITHUB_TOKEN` (automatic) | Everything else: labels, comments, commits, PR edits.          |
| `AUTOMATION_TOKEN`         | Optional. Only opens the PR and marks it ready, so CI runs.    |

`AUTOMATION_TOKEN` is never put in the environment of a step that runs an
agent, and never written into the checkout's git config. Issue bodies, PR
descriptions and review comments are untrusted input, and an agent reading them
should not hold a token that reaches beyond this repository. An agent's blast
radius is the `permissions:` block of its job.

Two more guards for the same reason:

- `issue-plan.yml` only runs automatically for issues opened by the repository
  owner, a member or a collaborator. Anyone else's issue waits for a maintainer
  to start the workflow by hand — an issue can be opened by anyone, and that
  text is fed to an agent.
- `pr-feedback.yml` ignores reviews from bots, so a review agent cannot restart
  the implementation loop. Only a human requesting changes does that.

Every prompt also tells the agent that the text it is reading is data, not
instructions, and to report anything that tries to redirect it.

## Working with it

**Normal loop.** Open an issue, read the plan, answer anything under "Decisions
needed", add `agent:ready`. Come back to a PR. Review it: request changes to
send it round again, merge when you are happy.

**Re-run one issue**: *Agent — implement* → **Run workflow**, with the issue
number, and `implement` or `revise`.

**Stop an agent**: cancel the run, then remove `agent:in-progress`. Nothing
restarts on its own — there is no poll.

**Take over a branch by hand.** The branch is `claude/issue-<number>` and it is
an ordinary branch — push to it, and the agent will build on your commits the
next time it runs.

## Labels

| Label                  | Means                                                        |
| ---------------------- | ------------------------------------------------------------ |
| `agent:ready`          | **You add this.** It starts an agent.                        |
| `agent:in-progress`    | An agent has it. Also what stops a second one starting.       |
| `agent:in-review`      | Implementation finished, PR is up.                            |
| `agent:planned`        | A plan comment has been posted.                               |
| `agent:needs-decision` | The plan is blocked on an answer. Do not add `agent:ready` yet. |
| `agent:blocked`        | A run failed. The comment on the issue links the log.         |

`.github/scripts/status.sh` is the only thing that writes them, and it keeps
exactly one status label on an issue at a time.

## When something goes wrong

**Nothing happens when I add `agent:ready`.** Check the Actions tab for *Agent
— implement*. If no run appears at all, the workflow is not on the default
branch yet — see setup step 3.

**A Claude job finished in seconds having done nothing.** Read its log for
"Workflow validation failed". The workflow file differs from the copy on the
default branch, which is the case on any PR that touches `.github/workflows/`.
Merge it and the workflow starts working.

**A run failed and the issue is stuck at `agent:in-progress`.** It also has
`agent:blocked` and a comment linking the log. Fix the cause, then remove
`agent:in-progress` and add `agent:ready` again.

**Agent PRs have no CI checks.** Expected without `AUTOMATION_TOKEN` — see
"The one optional extra". The implement workflow still ran `make check`.

**An artifact upload says "No files were found".**
`actions/upload-artifact` skips hidden files by default, so a path that is
itself hidden matches nothing even when it is full of files. This is why the
captures go to `shots/` rather than `.shots/`. Keep new output directories
visible.

**Two agents on one issue.** Should not happen: `concurrency` groups runs by
issue number, and `agent:in-progress` is set before any work starts. If it
does, cancel one — the branch is shared and the second run reuses it.

## Cost

Agent runs cost Claude usage; the workflows themselves are a few Actions
minutes. There is no idle cost at all, because nothing polls. If it is more
than you want, drop `synchronize` from `claude-code-review.yml` so the inline
review runs once per hand-off rather than on every push.
