#!/usr/bin/env bash
# shots.sh — capture the standard tour of the UI into shots/.
#
# Run this after a change that touches rendering or key handling, then read the
# .txt files (or paste the relevant one into the PR) to show what the UI looks
# like now. `make shots` builds the binary first and calls this.
#
#   scripts/shots.sh            # the standard scenes
#   scripts/shots.sh --svg      # also render colour SVGs for artifacts
#
# Add a scene by appending a line to the list below. Keep names numbered so the
# directory listing reads in tour order.
set -euo pipefail

cd "$(dirname "$0")/.."

SHOT=scripts/tui-shot.sh
OUT="${TUI_SHOT_DIR:-shots}"
EXTRA=()
[ "${1:-}" = "--svg" ] && EXTRA+=(--svg)

rm -rf "$OUT"
mkdir -p "$OUT"

shot() {
	local name="$1" keys="${2:-}"
	if "$SHOT" --name "$name" --keys "$keys" --out "$OUT" "${EXTRA[@]}"; then
		return 0
	fi
	printf '%s\n' "shots: scene $name failed" >&2
	failed=1
}

failed=0

shot 01-sprint ""
shot 02-dashboard "1"
shot 03-board "3"
shot 04-backlog "4"
shot 05-details "j,D"
shot 06-state-popup "j,s"
shot 07-filter "/,type:trace,Enter"
shot 08-description "j,j,d"
shot 09-discussion "j,D,C"
shot 10-comment-composer "j,D,C,c,i,type:Looks good to me,esc,ctrl+s"
shot 11-help "?"
shot 12-attention ":,type:attention,Enter"

if [ "$failed" = 1 ]; then
	printf '%s\n' "shots: at least one scene failed — see above" >&2
	exit 1
fi

printf '%s\n' "shots: $(ls "$OUT"/*.txt | wc -l) scenes in $OUT/"
