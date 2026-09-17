#!/usr/bin/env bash
# project.sh — read and write the "Devopstui Kanban" board from a workflow.
#
# GitHub Projects (v2) is not part of the repository, so none of this works
# with the automatic GITHUB_TOKEN: every call needs a token that carries the
# project scope. Export it as GH_TOKEN before calling (the workflows pass
# secrets.PROJECTS_TOKEN).
#
# Commands:
#   project.sh id                        print the project's node id
#   project.sh status <issue>            print an issue's Status ("" if not on the board)
#   project.sh set-status <issue> <name> put an issue on the board at that Status
#   project.sh list [status]             TSV: number, type, status, state, itemId
#   project.sh options                   list the Status field's options
#   project.sh doctor                    check token, project and field wiring
#
# Configuration (all optional, sensible defaults from the workflow context):
#   PROJECT_OWNER         defaults to the owner half of GITHUB_REPOSITORY
#   PROJECT_TITLE         defaults to "Devopstui Kanban"
#   PROJECT_NUMBER        set this to skip the title lookup entirely
#   PROJECT_STATUS_FIELD  defaults to "Status"
set -euo pipefail

PROJECT_TITLE="${PROJECT_TITLE:-Devopstui Kanban}"
PROJECT_STATUS_FIELD="${PROJECT_STATUS_FIELD:-Status}"
REPO="${GITHUB_REPOSITORY:-}"
PROJECT_OWNER="${PROJECT_OWNER:-${REPO%%/*}}"
REPO_NAME="${REPO#*/}"

CACHE_DIR="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/project-sh-cache"
mkdir -p "$CACHE_DIR"

die() { printf '%s\n' "project.sh: $*" >&2; exit 1; }

[ -n "$PROJECT_OWNER" ] || die "set PROJECT_OWNER or GITHUB_REPOSITORY"
command -v gh >/dev/null 2>&1 || die "gh is not installed"
command -v jq >/dev/null 2>&1 || die "jq is not installed"

gql() { gh api graphql "$@"; }

# --- project lookup ----------------------------------------------------------

# A user project and an organisation project are different GraphQL roots and
# the owner may be either, so try the one that matches and fall through.
lookup_projects() {
	local root="$1"
	gql -f owner="$PROJECT_OWNER" -f query='
		query($owner: String!) {
			'"$root"'(login: $owner) {
				projectsV2(first: 100) { nodes { id title number } }
			}
		}' 2>/dev/null | jq -c ".data.${root}.projectsV2.nodes // []"
}

resolve_project() {
	local cache="$CACHE_DIR/project.json"
	if [ -s "$cache" ]; then cat "$cache"; return; fi

	local nodes found=""
	nodes=$(lookup_projects user)
	[ "${nodes:-[]}" = "[]" ] && nodes=$(lookup_projects organization)
	[ -n "${nodes:-}" ] && [ "$nodes" != "[]" ] ||
		die "no projects visible to this token for owner '$PROJECT_OWNER'.
  The token needs the 'project' scope (classic PAT) or read/write Projects
  permission (fine-grained PAT). See docs/automation.md."

	if [ -n "${PROJECT_NUMBER:-}" ]; then
		found=$(jq -c --argjson n "$PROJECT_NUMBER" 'map(select(.number == $n)) | first // empty' <<<"$nodes")
		[ -n "$found" ] || die "owner '$PROJECT_OWNER' has no project number $PROJECT_NUMBER"
	else
		# Match on title, ignoring case so "Devopstui kanban" still resolves.
		found=$(jq -c --arg t "$PROJECT_TITLE" \
			'map(select((.title | ascii_downcase) == ($t | ascii_downcase))) | first // empty' <<<"$nodes")
		[ -n "$found" ] || die "owner '$PROJECT_OWNER' has no project titled '$PROJECT_TITLE'.
  Projects this token can see: $(jq -r 'map(.title) | join(", ")' <<<"$nodes")
  Set PROJECT_TITLE or PROJECT_NUMBER to point at the right one."
	fi

	printf '%s' "$found" | tee "$cache"
}

project_id() { resolve_project | jq -r '.id'; }

# --- status field ------------------------------------------------------------

status_field() {
	local cache="$CACHE_DIR/status-field.json"
	if [ -s "$cache" ]; then cat "$cache"; return; fi

	local field
	field=$(gql -f project="$(project_id)" -f query='
		query($project: ID!) {
			node(id: $project) {
				... on ProjectV2 {
					fields(first: 50) {
						nodes {
							... on ProjectV2SingleSelectField { id name options { id name } }
						}
					}
				}
			}
		}' | jq -c --arg f "$PROJECT_STATUS_FIELD" \
		'[.data.node.fields.nodes[] | select(.name != null)]
		 | map(select((.name | ascii_downcase) == ($f | ascii_downcase))) | first // empty')

	[ -n "$field" ] || die "project has no single-select field named '$PROJECT_STATUS_FIELD'"
	printf '%s' "$field" | tee "$cache"
}

option_id() {
	local want="$1" id
	id=$(status_field | jq -r --arg n "$want" \
		'first(.options[] | select((.name | ascii_downcase) == ($n | ascii_downcase)) | .id) // empty')
	[ -n "$id" ] || die "Status has no option '$want'. Options: $(status_field | jq -r '[.options[].name] | join(", ")')"
	printf '%s' "$id"
}

# --- items -------------------------------------------------------------------

# all_items emits one compact JSON object per project item that holds an issue
# or a PR, following pagination to the end of the board.
all_items() {
	local cursor="" page pid
	pid=$(project_id)
	while :; do
		if [ -n "$cursor" ]; then
			page=$(gql -f project="$pid" -f cursor="$cursor" -f query="$ITEMS_QUERY")
		else
			page=$(gql -f project="$pid" -f query="$ITEMS_QUERY")
		fi
		jq -c '.data.node.items.nodes[]
			| select(.content != null and .content.number != null)
			| {item: .id, number: .content.number, type: .content.__typename,
			   state: .content.state, status: (.fieldValue.name // "")}' <<<"$page"
		[ "$(jq -r '.data.node.items.pageInfo.hasNextPage' <<<"$page")" = "true" ] || break
		cursor=$(jq -r '.data.node.items.pageInfo.endCursor' <<<"$page")
	done
}

ITEMS_QUERY='
	query($project: ID!, $cursor: String) {
		node(id: $project) {
			... on ProjectV2 {
				items(first: 100, after: $cursor) {
					pageInfo { hasNextPage endCursor }
					nodes {
						id
						fieldValue: fieldValueByName(name: "'"$PROJECT_STATUS_FIELD"'") {
							... on ProjectV2ItemFieldSingleSelectValue { name }
						}
						content {
							__typename
							... on Issue { number state }
							... on PullRequest { number state }
						}
					}
				}
			}
		}
	}'

# Slurped rather than piped through head: closing the pipe early would break
# the stream with SIGPIPE, and pipefail would turn that into a script failure.
item_for_issue() {
	all_items | jq -s -c --argjson n "$1" \
		'map(select(.type == "Issue" and .number == $n)) | first // empty'
}

issue_node_id() {
	gql -f owner="$PROJECT_OWNER" -f repo="$REPO_NAME" -F number="$1" -f query='
		query($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) { issue(number: $number) { id } }
		}' | jq -r '.data.repository.issue.id // empty'
}

add_issue() {
	local content
	content=$(issue_node_id "$1")
	[ -n "$content" ] || die "issue #$1 not found in $REPO"
	gql -f project="$(project_id)" -f content="$content" -f query='
		mutation($project: ID!, $content: ID!) {
			addProjectV2ItemById(input: {projectId: $project, contentId: $content}) { item { id } }
		}' | jq -r '.data.addProjectV2ItemById.item.id'
}

set_status() {
	local issue="$1" status="$2" item field field_id option pid
	# Resolve everything into variables first: a lookup that fails inside an
	# argument list would otherwise leave set -e happy and send the mutation
	# with an empty value.
	pid=$(project_id)
	field=$(status_field)
	field_id=$(jq -r '.id' <<<"$field")
	option=$(option_id "$status")
	item=$(item_for_issue "$issue" | jq -r '.item // empty')
	if [ -z "$item" ]; then
		# Not on the board yet — an issue opened before the project existed, or
		# one nobody triaged. Adding it here is what makes the board the single
		# place the workflow is driven from.
		item=$(add_issue "$issue")
	fi
	gql -f project="$pid" -f item="$item" \
		-f field="$field_id" -f option="$option" -f query='
		mutation($project: ID!, $item: ID!, $field: ID!, $option: String!) {
			updateProjectV2ItemFieldValue(input: {
				projectId: $project, itemId: $item, fieldId: $field,
				value: {singleSelectOptionId: $option}
			}) { projectV2Item { id } }
		}' >/dev/null
	printf '%s\n' "project.sh: #$issue -> $status"
}

# --- commands ----------------------------------------------------------------

case "${1:-}" in
id) project_id ;;
status)
	[ $# -ge 2 ] || die "usage: project.sh status <issue>"
	item_for_issue "$2" | jq -r '.status // ""'
	;;
set-status)
	[ $# -ge 3 ] || die "usage: project.sh set-status <issue> <status>"
	set_status "$2" "$3"
	;;
add)
	[ $# -ge 2 ] || die "usage: project.sh add <issue>"
	add_issue "$2"
	;;
list)
	want="${2:-}"
	all_items | jq -r --arg want "$want" \
		'select($want == "" or (.status | ascii_downcase) == ($want | ascii_downcase))
		 | [(.number | tostring), .type, .status, .state, .item] | @tsv'
	;;
options) status_field | jq -r '.options[].name' ;;
doctor)
	printf 'owner:   %s\n' "$PROJECT_OWNER"
	printf 'project: %s (#%s)\n' "$(resolve_project | jq -r .title)" "$(resolve_project | jq -r .number)"
	printf 'id:      %s\n' "$(project_id)"
	printf 'field:   %s\n' "$(status_field | jq -r .name)"
	printf 'options: %s\n' "$(status_field | jq -r '[.options[].name] | join(", ")')"
	printf 'items:   %s on the board\n' "$(all_items | wc -l | tr -d ' ')"
	;;
*)
	sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
