#!/usr/bin/env bash
# settle-plan.test.sh — what settle-plan.sh does, asserted.
#
# The pipeline's shell is not Go, so `go test` never sees it, and two rounds of
# review found real bugs in this particular logic: an unanchored issue number
# that let a marker for #160 satisfy #16, and a body-only check that let anyone
# who can comment pass an issue off as planned. Both are regressions worth
# catching, so they are cases below.
#
# settle-plan.sh is copied next to a stub status.sh, because it resolves that
# one as its own sibling. gh is stubbed on PATH and serves a comments fixture
# through the real --jq, so the bot filter is exercised rather than assumed.
#
#   .github/scripts/settle-plan.test.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# jq is a hard dependency of the pipeline (status.sh dies without it) and is on
# every CI runner, so this only ever skips on a developer machine that lacks it.
if ! command -v jq >/dev/null 2>&1; then
	echo "settle-plan.test: jq is not installed — skipping."
	exit 0
fi

sandbox=$(mktemp -d)
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin" "$sandbox/scripts"
cp "$here/settle-plan.sh" "$sandbox/scripts/"

# gh stub. `api` serves $FIXTURE through whatever --jq the script passed;
# GH_MODE=fail makes the read fail instead. `issue comment` just records itself.
cat >"$sandbox/bin/gh" <<'STUB'
#!/usr/bin/env bash
if [ "$1" = "api" ]; then
	[ "${GH_MODE:-ok}" = "fail" ] && { echo "gh: HTTP 403" >&2; exit 1; }
	prev=""; jqexpr=""
	for a in "$@"; do [ "$prev" = "--jq" ] && jqexpr="$a"; prev="$a"; done
	jq -r "$jqexpr" "$FIXTURE"
	exit 0
fi
if [ "$1" = "issue" ] && [ "$2" = "comment" ]; then
	echo "COMMENTED"
	exit 0
fi
echo "gh stub: unexpected: $*" >&2
exit 1
STUB

# status.sh stub. STATUS_FAIL names a flag whose `add` should fail, for the
# "plan is there but labelling broke" case.
cat >"$sandbox/scripts/status.sh" <<'STUB'
#!/usr/bin/env bash
if [ "${STATUS_FAIL:-}" = "$4" ] && [ "$3" = "add" ]; then
	echo "status stub: simulated failure on $*" >&2
	exit 1
fi
echo "LABEL $3 $4"
STUB
chmod +x "$sandbox/bin/gh" "$sandbox/scripts/status.sh"

fixture() { printf '%s' "$2" >"$sandbox/$1.json"; }
fixture empty '[]'
fixture marker '[{"user":{"type":"Bot"},"body":"<!-- claude-issue-plan: 16 -->\n\n## Plan\n\nreal"}]'
fixture heading_only '[{"user":{"type":"Bot"},"body":"## Plan\n\nmarker dropped"}]'
fixture heading_crlf '[{"user":{"type":"Bot"},"body":"## Plan\r\n\r\nfrom the browser"}]'
fixture planted_marker '[{"user":{"type":"User"},"body":"lol <!-- claude-issue-plan: 16 -->"}]'
fixture planted_heading '[{"user":{"type":"User"},"body":"## Plan\n\nnot the agent"}]'
fixture marker_160 '[{"user":{"type":"Bot"},"body":"see <!-- claude-issue-plan: 160 -->"}]'
fixture chatter '[{"user":{"type":"Bot"},"body":"we should plan this\n## Planning notes"}]'

pass=0
fail=0

# check <name> <fixture> <want-exit> <want-substring> [issue] [gh-mode] [status-fail]
check() {
	local name=$1 fx=$2 want_code=$3 want_out=$4
	local issue=${5:-16} ghmode=${6:-ok} statusfail=${7:-}
	local out code
	out=$(
		cd "$sandbox" || exit 99
		PATH="$sandbox/bin:$PATH" ISSUE="$issue" REPO=owner/name OUTCOME=success \
			RUN_URL=https://example/run FIXTURE="$sandbox/$fx.json" \
			GH_MODE="$ghmode" STATUS_FAIL="$statusfail" \
			bash scripts/settle-plan.sh 2>&1
	) && code=0 || code=$?

	if [ "$code" = "$want_code" ] && grep -qF "$want_out" <<<"$out"; then
		printf '  ok    %s\n' "$name"
		pass=$((pass + 1))
		return
	fi
	printf '  FAIL  %s\n' "$name"
	printf '        want exit %s containing %q\n' "$want_code" "$want_out"
	printf '        got  exit %s:\n' "$code"
	sed 's/^/          /' <<<"$out"
	fail=$((fail + 1))
}

echo "settle-plan: a plan is there"
check "bot posts the marker"            marker          0 "LABEL add planned"
check "bot posts a plan, marker dropped" heading_only   0 "LABEL add planned"
check "heading with CRLF line endings"  heading_crlf    0 "LABEL add planned"
check "a stale block is cleared"        marker          0 "LABEL rm blocked"

echo "settle-plan: no plan is there"
check "no comments at all"              empty           1 "posted no plan comment"
check "no comments: says blocked"       empty           1 "LABEL add blocked"
check "no comments: comments on it"     empty           1 "COMMENTED"
check "a human plants the marker"       planted_marker  1 "posted no plan comment"
check "a human writes '## Plan'"        planted_heading 1 "posted no plan comment"
check "a marker for #160, read on #16"  marker_160      1 "posted no plan comment"
check "a bot merely mentioning a plan"  chatter         1 "posted no plan comment"

echo "settle-plan: it cannot tell"
check "the comments cannot be read"     empty           1 "Could not read"        16 fail
check "the number is not a number"      empty           1 "Not an issue number"   "16;rm -rf /"
check "the plan is there, labelling is not" marker      1 "has a plan, but labelling it failed" 16 ok planned

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
