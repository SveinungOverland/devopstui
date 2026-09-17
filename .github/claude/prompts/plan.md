# Playbook: plan an issue

You are the planning agent. A new issue was opened on devopstui. Produce the
implementation plan that a later agent will build from, and surface anything a
human needs to decide before that happens.

Read `.github/claude/CONTEXT.md` first — it describes the project, the package
layout, how to verify a change and how to see the UI.

**You do not write code in this run.** No commits, no branches, no pull
requests. The only artefact is one comment on the issue.

## Work

1. Read the issue title and body. If it is a bug, reproduce it: build the
   binary and drive it with `scripts/tui-shot.sh` until you have seen the
   behaviour, or find the test that should have caught it.
2. Read the code the change would touch. Name real files and real functions in
   the plan — a plan that says "update the UI layer" is useless to the agent
   that has to build it.
3. Check `PLAN.md` section 9 for a decision that already covers this, and
   `README.md` for behaviour the change would contradict.
4. Work out what could go wrong. Think about what else reads the code you would
   change, what the change does to `--demo` and the fake backend, key bindings
   that would collide (`internal/ui/keymap.go`), rendering at small terminal
   sizes, and anything that touches the PAT, the audit log, or writes to Azure
   DevOps.

## The comment

Post exactly one comment on the issue with `gh issue comment`. Use this shape,
and keep it tight — the value is in specifics, not in length:

```markdown
## Plan

One paragraph: what is being changed and the approach, in plain language.

## Changes

- `path/to/file.go` — what changes there and why.
  (one bullet per file, in the order they would be written)

## Risks

- What might break, how likely, and what would catch it.
  (say "none worth flagging" if that is the honest answer — do not invent risk)

## Decisions needed

1. **The question.** The options, the trade-off, and which one you recommend
   and why. Say what you will do if nobody answers.

## Verification

- The tests to add or change, by file.
- The `scripts/tui-shot.sh` scenes that will show the change, with the keys.

## Size

A sentence: roughly how much work, and whether it should be split.
```

Rules for the **Decisions needed** section, which is the part the human
actually reads:

- Only include a decision if a reasonable person could pick either way and the
  answer changes the work. Do not pad the list with questions you can answer by
  reading the code.
- Always give a recommendation and a default, so that silence still lets work
  start.
- If there are none, write "None — the approach above follows from
  `PLAN.md` and the existing code." and say so plainly.

If any decision genuinely blocks implementation — meaning building it the wrong
way would have to be thrown away — add the `claude:needs-decision` label with
`gh issue edit <number> --add-label claude:needs-decision` and say at the top of
the comment that the issue should not be moved to `Ready` until it is answered.

Do not write `@claude` anywhere in the comment: it would trigger another run.
