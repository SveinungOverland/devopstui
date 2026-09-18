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

# Find the branch this issue is already being worked on, if any. The name is
# not always `claude/issue-<n>`: claude-code-action's own default naming appends
# a timestamp, so work started by an `@claude` mention lands on something like
# `claude/issue-42-20260917-1830`. Guessing one exact name would strand that
# work and open a second pull request beside it.
#
# An open pull request is the most reliable answer, because that is the thing
# the rest of the pipeline drives. Fall back to the branch list, then to
# creating one.
pr=$(gh pr list --state open --json number,headRefName \
	--jq "[.[] | select(.headRefName | test(\"^claude/issue-$ISSUE(-|$)\"))] | first // empty")

if [ -n "$pr" ]; then
	BRANCH=$(jq -r .headRefName <<<"$pr")
	pr=$(jq -r .number <<<"$pr")
	git fetch origin "$BRANCH"
	git checkout -B "$BRANCH" "origin/$BRANCH"
	printf '%s\n' "start-work: reusing branch $BRANCH from open PR #$pr"
else
	# Reverse-sorted so a timestamped branch, which carries real work, wins
	# over a bare `claude/issue-<n>` that may be an empty scaffold.
	matches=$(git ls-remote --heads origin \
		"refs/heads/claude/issue-$ISSUE" "refs/heads/claude/issue-$ISSUE-*" |
		awk '{print $2}' | sed 's|refs/heads/||' | sort -r)
	# First line without a pipe into head: closing that pipe early would trip
	# pipefail.
	found="${matches%%$'\n'*}"

	if [ -n "$found" ]; then
		BRANCH="$found"
		git fetch origin "$BRANCH"
		git checkout -B "$BRANCH" "origin/$BRANCH"
		printf '%s\n' "start-work: reusing existing branch $BRANCH"
	else
		git fetch origin "$base"
		git checkout -B "$BRANCH" "origin/$base"
		# A pull request needs at least one commit between head and base. This
		# empty one exists so the PR can be opened now, at the start of the
		# work, rather than after the fact — the issue is labelled
		# `agent:in-progress` and there is already somewhere to watch it happen.
		git commit --allow-empty -m "Start work on #$ISSUE: $title"
		git push -u origin "$BRANCH"
		printf '%s\n' "start-work: created branch $BRANCH from $base"
	fi
	pr=$(gh pr list --head "$BRANCH" --state open --json number --jq '.[0].number // empty')
fi

if [ -z "$pr" ]; then
	body=$(
		cat <<-EOF
			Closes #$ISSUE

			🤖 Opened automatically when #$ISSUE was labelled \`agent:ready\`.
			An agent is implementing it now; this description is rewritten when the
			work lands. Convert this pull request back to a draft — or request
			changes on it — to send it back for another pass.
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
		# workflows from running against a half-finished branch. Usually a
		# no-op now, since converting to draft is itself one of the signals
		# that starts a revise run; when it does change the state, the issue is
		# already `agent:in-progress` and pr-feedback.yml ignores the event.
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
