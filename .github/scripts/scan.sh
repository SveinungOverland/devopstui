#!/usr/bin/env bash
# scan.sh — work out what the board is asking for, and emit it as two JSON
# arrays for project-sync.yml to fan out over.
#
# GitHub cannot trigger a repository workflow when a project card moves —
# projects_v2_item webhooks only exist at organisation level and are not a
# workflow trigger — so the board is polled and reconciled instead. This script
# is the whole reconciliation: it compares what the board says against what has
# actually happened, and reports the gap.
#
#   implement=["12","15"]   cards at Ready with no agent already on them
#   review=["31"]           ready PRs whose current commit has not been reviewed
#
# Drift it fixes directly, because there is nothing to dispatch: a closed issue
# that is not yet at Done.
#
# Needs GH_TOKEN with repo and project access.
set -euo pipefail

cd "$(dirname "$0")/../.."

REPO="${GITHUB_REPOSITORY:?set GITHUB_REPOSITORY}"
# A safety valve: if someone moves ten cards to Ready at once, start a couple
# and let the next poll pick up the rest, rather than launching ten agents.
MAX_IMPLEMENT="${MAX_IMPLEMENT:-2}"
MAX_REVIEW="${MAX_REVIEW:-3}"

project=".github/scripts/project.sh"
implement=()
review=()
notes=()

# The claim label has to exist before it can be applied. Harmless if it is
# already there, and it means a repository that never ran the bootstrap
# workflow still gets a working duplicate guard.
gh label create "claude:implementing" --color 5319e7 \
	--description "An agent is working on this right now" --force >/dev/null 2>&1 || true

has_label() {
	gh issue view "$1" --json labels --jq '[.labels[].name] | index("'"$2"'") != null' 2>/dev/null
}

# reviewed_at_head answers "has the impact reviewer already seen this commit",
# which is what stops a card parked at In review from being reviewed every
# five minutes for as long as it sits there.
reviewed_at_head() {
	local pr="$1" sha="$2" bodies
	# Collected before grepping: piping straight into `grep -q` lets grep close
	# the pipe on the first match, and pipefail would then report the whole
	# thing as failed — which would look like "not reviewed" and start a review
	# on every poll, forever.
	bodies=$(gh api "repos/$REPO/issues/$pr/comments" --paginate --jq '.[].body' 2>/dev/null || true)
	grep -qF "claude-impact-review: $sha" <<<"$bodies"
}

# Read the board up front so that a failure here stops the run, rather than
# looking like an empty board and reconciling nothing.
board=$("$project" list)

while IFS=$'\t' read -r number type status state _item; do
	[ "$type" = "Issue" ] || continue

	case "$state:$status" in
	CLOSED:Done) ;;
	CLOSED:*)
		# Closed by hand, or closed by a merge the PR workflow missed.
		"$project" set-status "$number" "Done"
		notes+=("#$number closed while at '$status' → Done")
		;;
	esac
	[ "$state" = "OPEN" ] || continue

	case "$(printf '%s' "$status" | tr '[:upper:]' '[:lower:]')" in
	ready)
		if [ "$(has_label "$number" "claude:implementing")" = "true" ]; then
			notes+=("#$number is Ready but already has an agent on it — skipped")
			continue
		fi
		if [ "${#implement[@]}" -lt "$MAX_IMPLEMENT" ]; then
			# Claimed here rather than in the workflow that does the work: the
			# minute between this scan and that job starting is long enough for
			# the next poll to pick the same issue up a second time.
			gh issue edit "$number" --add-label "claude:implementing" >/dev/null
			implement+=("$number")
		else
			notes+=("#$number is Ready but over the per-run limit — next poll")
		fi
		;;
	"in review")
		pr=$(gh pr list --head "claude/issue-$number" --state open \
			--json number,isDraft,headRefOid --jq '.[0] // empty' 2>/dev/null || true)
		if [ -z "$pr" ]; then
			notes+=("#$number is In review with no open PR — needs a look")
			continue
		fi
		[ "$(jq -r .isDraft <<<"$pr")" = "false" ] || continue
		pr_number=$(jq -r .number <<<"$pr")
		sha=$(jq -r .headRefOid <<<"$pr")
		if reviewed_at_head "$pr_number" "$sha"; then continue; fi
		if [ "${#review[@]}" -lt "$MAX_REVIEW" ]; then
			review+=("$pr_number")
		else
			notes+=("PR #$pr_number needs a review but is over the per-run limit")
		fi
		;;
	esac
done <<<"$board"

as_json() {
	if [ "$#" -eq 0 ]; then printf '[]'; else printf '%s\n' "$@" | jq -R . | jq -s -c .; fi
}

implement_json=$(as_json ${implement[@]+"${implement[@]}"})
review_json=$(as_json ${review[@]+"${review[@]}"})

if [ -n "${GITHUB_OUTPUT:-}" ]; then
	{
		printf 'implement=%s\n' "$implement_json"
		printf 'review=%s\n' "$review_json"
	} >>"$GITHUB_OUTPUT"
fi

{
	printf '### Board scan\n\n'
	printf -- '- implement: `%s`\n' "$implement_json"
	printf -- '- review: `%s`\n' "$review_json"
	if [ "${#notes[@]}" -gt 0 ]; then
		printf '\n'
		printf -- '- %s\n' "${notes[@]}"
	fi
} | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
