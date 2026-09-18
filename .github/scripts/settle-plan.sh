#!/usr/bin/env bash
# settle-plan.sh — judge a planning run on what it produced, and label the
# issue accordingly.
#
# `agent:planned` means "a plan comment exists", so it is applied only once one
# does. An agent exiting without an error is not evidence of that: a run that
# stops early finishes green, and labelling on the step's outcome alone once put
# `agent:planned` on an issue with no plan under it.
#
# Exits non-zero when no plan is there, so the run goes red rather than green
# over an empty issue. issue-plan.yml calls this as its last step.
#
# Needs GH_TOKEN with issues: write. REPO is passed to gh explicitly, so this
# works even when the checkout failed and there is no git remote to infer.
#
#   ISSUE=42 REPO=owner/name OUTCOME=success RUN_URL=https://... settle-plan.sh
set -euo pipefail

ISSUE="${ISSUE:?set ISSUE to the issue number}"
REPO="${REPO:?set REPO to owner/name}"
OUTCOME="${OUTCOME:-unknown}"
RUN_URL="${RUN_URL:-}"

# status.sh is this script's sibling, whatever directory the caller is in.
status="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/status.sh"

# The number is interpolated into a URL and a regex below, and on a
# workflow_dispatch it is whatever was typed into the form.
[[ "$ISSUE" =~ ^[0-9]+$ ]] || {
	echo "::error::Not an issue number: '$ISSUE'"
	exit 1
}

# Only comments left by a bot count. This repository is public, so anyone can
# comment on an issue, and a body-only check would let a passer-by post the
# marker and pass the issue off as planned — the very state this exists to rule
# out. It also keeps a human writing "## Plan" in a reply from tripping the
# fallback below.
#
# A read that fails is its own failure and is reported as one — treating it as
# "no comments" would blame the agent for a plan it may well have posted.
if ! bodies=$(gh api "repos/$REPO/issues/$ISSUE/comments" \
	--paginate --jq '.[] | select(.user.type == "Bot") | .body'); then
	echo "::error::Could not read the comments on #$ISSUE."
	exit 1
fi

# The marker is what plan.md asks for. `([^0-9]|$)` anchors the number so #16 is
# not satisfied by a marker for #160 — unlike agent-review.yml's 40-character
# SHA, a short issue number is a prefix of plenty of others. The `## Plan`
# heading is a fallback, so a plan that is right in every other way is never
# reported missing over one dropped line; `[[:space:]]*$` tolerates the CRLF the
# API returns for a comment that was written in the browser. Both must keep
# matching the comment shape in plan.md.
if grep -qE "claude-issue-plan: $ISSUE([^0-9]|$)" <<<"$bodies" ||
	grep -qE '^## Plan[[:space:]]*$' <<<"$bodies"; then
	# No `|| true` here, unlike the cleanup below: a plan that cannot be
	# labelled has to fail, or `agent:planned` quietly stops meaning anything,
	# which is the bug this exists to fix. Say which way it went, so a red run
	# is not read as a missing plan.
	"$status" flag "$ISSUE" add planned || {
		echo "::error::#$ISSUE has a plan, but labelling it failed."
		exit 1
	}
	# Clearing a stale block from an earlier failed run is cleanup, so it never
	# decides the outcome of this one.
	"$status" flag "$ISSUE" rm blocked || true
	exit 0
fi

"$status" flag "$ISSUE" add blocked || true
body=$(mktemp)
trap 'rm -f "$body"' EXIT
{
	printf 'The planning run finished as **%s** without posting a plan.\n' "$OUTCOME"
	printf '\nThe issue is **not** labelled `agent:planned` — there is no plan to read —\n'
	printf 'and carries `agent:blocked` instead. Do not add `agent:ready` until a plan\n'
	printf 'is here.\n'
	printf '\n[Run log](%s). Re-run *Agent — plan an issue* with this issue number to\n' "$RUN_URL"
	printf 'try again.\n'
} >"$body"
gh issue comment "$ISSUE" --repo "$REPO" --body-file "$body"
echo "::error::The planning run for #$ISSUE posted no plan comment."
exit 1
