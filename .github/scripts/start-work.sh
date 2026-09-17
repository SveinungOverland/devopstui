#!/usr/bin/env bash
# start-work.sh — make sure an issue has a branch and a draft pull request to
# work on, and leave both on stdout / $GITHUB_OUTPUT.
#
# Idempotent: a second run on the same issue reuses the branch and the PR, so a
# retried or resumed workflow does not open a second pull request.
#
# Needs GH_TOKEN with repo access. Expects to run inside a full checkout.
#
#   ISSUE=42 MODE=implement .github/scripts/start-work.sh
set -euo pipefail

ISSUE="${ISSUE:?set ISSUE to the issue number}"
MODE="${MODE:-implement}"
BRANCH="claude/issue-$ISSUE"

die() { printf '%s\n' "start-work: $*" >&2; exit 1; }

command -v gh >/dev/null 2>&1 || die "gh is not installed"

base=$(gh repo view --json defaultBranchRef --jq .defaultBranchRef.name)
title=$(gh issue view "$ISSUE" --json title --jq .title)
[ -n "$title" ] || die "issue #$ISSUE not found"

# Commits are attributed to the Actions bot rather than to whoever owns the
# token, so history says plainly that a workflow wrote them.
git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

if git ls-remote --exit-code --heads origin "$BRANCH" >/dev/null 2>&1; then
	git fetch origin "$BRANCH"
	git checkout -B "$BRANCH" "origin/$BRANCH"
	printf '%s\n' "start-work: reusing branch $BRANCH"
else
	git fetch origin "$base"
	git checkout -B "$BRANCH" "origin/$base"
	# A pull request needs at least one commit between head and base. This
	# empty one exists so the PR can be opened now, at the start of the work,
	# rather than after the fact — the issue is labelled `agent:in-progress`
	# and there is already somewhere to watch it happen.
	git commit --allow-empty -m "Start work on #$ISSUE: $title"
	git push -u origin "$BRANCH"
	printf '%s\n' "start-work: created branch $BRANCH from $base"
fi

pr=$(gh pr list --head "$BRANCH" --state open --json number --jq '.[0].number // empty')

if [ -z "$pr" ]; then
	body=$(
		cat <<-EOF
			Closes #$ISSUE

			🤖 Opened automatically when #$ISSUE was labelled \`agent:ready\`.
			An agent is implementing it now; this description is rewritten when the
			work lands. Request changes on this PR to send it back for another pass.
		EOF
	)
	# gh pr create prints the new PR's URL and has no --json, so the number
	# comes off the end of the URL.
	url=$(gh pr create --draft --base "$base" --head "$BRANCH" \
		--title "$title" --body "$body")
	pr="${url##*/}"
	case "$pr" in
	'' | *[!0-9]*) die "could not read a PR number out of: $url" ;;
	esac
	printf '%s\n' "start-work: opened draft PR #$pr"
else
	printf '%s\n' "start-work: reusing PR #$pr"
	if [ "$MODE" = "revise" ]; then
		# Back to draft while the agent works, which also stops the review
		# workflows from running against a half-finished branch.
		gh pr ready "$pr" --undo >/dev/null 2>&1 || true
	fi
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
	{
		printf 'branch=%s\n' "$BRANCH"
		printf 'pr=%s\n' "$pr"
		printf 'base=%s\n' "$base"
	} >>"$GITHUB_OUTPUT"
fi
