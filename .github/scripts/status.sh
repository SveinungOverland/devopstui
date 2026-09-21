#!/usr/bin/env bash
# status.sh — the pipeline's control surface: one `agent:` label per issue.
#
# An issue is in exactly one status at a time, and the label says which. No
# project board, no GraphQL, no extra token: labels are part of the repository,
# so the workflow's own GITHUB_TOKEN can read and write them, and adding one
# raises an `issues: labeled` event that starts a workflow.
#
#   set <issue> <status>       ready | in-progress | in-review | none
#   get <issue>                print the current status, empty if none
#   flag <issue> add|rm <name> blocked | planned | needs-decision
#   ensure-labels              create every label this pipeline uses
#
# Needs GH_TOKEN with issues: write. REPO defaults to GITHUB_REPOSITORY and is
# passed explicitly to gh, so this works before a checkout exists.
set -euo pipefail

REPO="${REPO:-${GITHUB_REPOSITORY:-}}"
PREFIX="agent:"

die() { printf '%s\n' "status.sh: $*" >&2; exit 1; }

[ -n "$REPO" ] || die "set REPO or GITHUB_REPOSITORY"
command -v gh >/dev/null 2>&1 || die "gh is not installed"
command -v jq >/dev/null 2>&1 || die "jq is not installed"

# The single source of truth for the pipeline's labels: name, colour,
# description. Statuses are mutually exclusive; flags are independent.
STATUSES="ready in-progress in-review"
FLAGS="planned needs-decision blocked"
# Labels that live on a pull request rather than an issue. `set` and `flag` go
# through `gh issue edit`, which cannot touch a pull request, so nothing here
# writes them — pr-merge-main.yml removes its own with `gh pr edit`.
# ensure-labels creates them so they are in the picker with the rest.
PR_LABELS="merge-main"

label_colour() {
	case "$1" in
	ready) printf '0e8a16' ;;          # green — the human's go-ahead
	in-progress) printf '5319e7' ;;    # purple — an agent has it
	in-review) printf '1d76db' ;;      # blue — waiting on a human
	planned) printf 'c5def5' ;;        # pale blue — informational
	needs-decision) printf 'fbca04' ;; # yellow — waiting on an answer
	blocked) printf 'b60205' ;;        # red — something failed
	merge-main) printf 'd4c5f9' ;;     # pale purple — on a pull request
	*) printf 'ededed' ;;
	esac
}

label_description() {
	case "$1" in
	ready) printf 'Approved to build. Adding this label starts an agent.' ;;
	in-progress) printf 'An agent is working on this right now.' ;;
	in-review) printf 'Implementation finished, the pull request is up.' ;;
	planned) printf 'An implementation plan has been posted.' ;;
	needs-decision) printf 'The plan is waiting on a human decision.' ;;
	blocked) printf 'An automated run failed and needs a look.' ;;
	merge-main) printf 'On a PR: merge the default branch in, resolving conflicts.' ;;
	*) printf '' ;;
	esac
}

ensure_label() {
	gh label create "$PREFIX$1" --repo "$REPO" \
		--color "$(label_colour "$1")" \
		--description "$(label_description "$1")" \
		--force >/dev/null 2>&1 || true
}

issue_labels() {
	gh issue view "$1" --repo "$REPO" --json labels --jq '.labels[].name' 2>/dev/null || true
}

cmd_set() {
	local issue="$1" want="$2" current args=() name
	case " $STATUSES none " in
	*" $want "*) ;;
	*) die "unknown status '$want' (want: $STATUSES none)" ;;
	esac

	current=$(issue_labels "$issue")

	# Only ever remove a label the issue actually has. gh rejects the whole
	# edit if asked to remove one the repository has never heard of, and that
	# would take a status change down with it.
	for name in $STATUSES; do
		[ "$name" = "$want" ] && continue
		grep -qxF "$PREFIX$name" <<<"$current" && args+=(--remove-label "$PREFIX$name")
	done

	if [ "$want" != "none" ]; then
		ensure_label "$want"
		args+=(--add-label "$PREFIX$want")
	fi

	if [ "${#args[@]}" -eq 0 ]; then
		printf '%s\n' "status.sh: #$issue already at '$want'"
		return
	fi
	gh issue edit "$issue" --repo "$REPO" "${args[@]}" >/dev/null
	printf '%s\n' "status.sh: #$issue -> $want"
}

cmd_get() {
	local current name
	current=$(issue_labels "$1")
	for name in $STATUSES; do
		if grep -qxF "$PREFIX$name" <<<"$current"; then
			printf '%s\n' "$name"
			return
		fi
	done
}

cmd_flag() {
	local issue="$1" action="$2" name="$3"
	case " $FLAGS " in
	*" $name "*) ;;
	*) die "unknown flag '$name' (want: $FLAGS)" ;;
	esac
	case "$action" in
	add)
		ensure_label "$name"
		gh issue edit "$issue" --repo "$REPO" --add-label "$PREFIX$name" >/dev/null
		;;
	rm)
		grep -qxF "$PREFIX$name" <<<"$(issue_labels "$issue")" || return 0
		gh issue edit "$issue" --repo "$REPO" --remove-label "$PREFIX$name" >/dev/null
		;;
	*) die "flag action must be add or rm" ;;
	esac
	printf '%s\n' "status.sh: #$issue $action $PREFIX$name"
}

case "${1:-}" in
set)
	[ $# -ge 3 ] || die "usage: status.sh set <issue> <status>"
	cmd_set "$2" "$3"
	;;
get)
	[ $# -ge 2 ] || die "usage: status.sh get <issue>"
	cmd_get "$2"
	;;
flag)
	[ $# -ge 4 ] || die "usage: status.sh flag <issue> add|rm <name>"
	cmd_flag "$2" "$3" "$4"
	;;
ensure-labels)
	for l in $STATUSES $FLAGS $PR_LABELS; do
		ensure_label "$l"
		printf '%s\n' "$PREFIX$l"
	done
	;;
*)
	sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
