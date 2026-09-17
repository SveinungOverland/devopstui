#!/usr/bin/env bash
# tui-shot.sh — run devopstui in a real terminal, press keys at it, and save
# what the screen looks like.
#
# A TUI's "screenshot" is the terminal grid, so the primary output is a .txt
# capture that drops straight into a PR comment inside a code fence. Pass
# --svg to also render a colour-accurate image for workflow artifacts.
#
#   scripts/tui-shot.sh --name board --keys 3,j,j,l
#   scripts/tui-shot.sh --name filter --keys /,type:trace,Enter --svg
#   scripts/tui-shot.sh --name editor --keys j,d --size 160x48 --settle 2
#
# Keys are comma-separated and sent one at a time, each followed by --delay
# seconds so the model has time to redraw:
#
#   j            a tmux key name or literal character (Enter, Escape, C-u, Space, q)
#   type:hello   type the literal text "hello" (use for filter/search input)
#   wait:1.5     pause 1.5s without sending anything
#
# Exits non-zero if the program dies before the last capture, leaving the dead
# pane's output in the .txt file so you can read the panic.
set -euo pipefail

NAME=""
KEYS=""
SIZE="140x40"
OUT_DIR="${TUI_SHOT_DIR:-shots}"
CMD="./bin/devopstui --demo"
DELAY="0.5"
SETTLE="2.5"
SVG=0

die() { printf '%s\n' "tui-shot: $*" >&2; exit 2; }

while [ $# -gt 0 ]; do
	case "$1" in
	--name) NAME="${2:-}"; shift 2 ;;
	--keys) KEYS="${2:-}"; shift 2 ;;
	--size) SIZE="${2:-}"; shift 2 ;;
	--out) OUT_DIR="${2:-}"; shift 2 ;;
	--cmd) CMD="${2:-}"; shift 2 ;;
	--delay) DELAY="${2:-}"; shift 2 ;;
	--settle) SETTLE="${2:-}"; shift 2 ;;
	--svg) SVG=1; shift ;;
	-h | --help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) die "unknown flag $1 (try --help)" ;;
	esac
done

[ -n "$NAME" ] || die "--name is required"
command -v tmux >/dev/null 2>&1 || die "tmux is not installed (apt-get install tmux)"

case "$SIZE" in
*x*) COLS="${SIZE%x*}"; ROWS="${SIZE#*x}" ;;
*) die "--size must look like 140x40" ;;
esac

case "$CMD" in
./bin/devopstui*)
	[ -x ./bin/devopstui ] || die "./bin/devopstui is missing — run: make build"
	;;
esac

mkdir -p "$OUT_DIR"
TXT="$OUT_DIR/$NAME.txt"
ANSI="$OUT_DIR/$NAME.ansi"

SESSION="tuishot-$NAME-$$"
cleanup() { tmux kill-session -t "$SESSION" 2>/dev/null || true; }
trap cleanup EXIT

# A detached session gives the program a real pty at a fixed size, so the
# layout is reproducible run to run. remain-on-exit keeps a crashed pane's
# output on screen for the capture below.
tmux new-session -d -s "$SESSION" -x "$COLS" -y "$ROWS" \
	-e TERM=xterm-256color -e COLORTERM=truecolor \
	-e VISUAL= -e EDITOR= -e NO_COLOR= \
	"$CMD"
tmux set-option -t "$SESSION" remain-on-exit on >/dev/null

sleep "$SETTLE"

send_key() {
	local key="$1"
	case "$key" in
	type:*) tmux send-keys -t "$SESSION" -l -- "${key#type:}" ;;
	wait:*) sleep "${key#wait:}"; return ;;
	"") return ;;
	*) tmux send-keys -t "$SESSION" -- "$key" ;;
	esac
	sleep "$DELAY"
}

if [ -n "$KEYS" ]; then
	# Split on commas only; a key is never allowed to contain one.
	IFS=',' read -r -a key_list <<<"$KEYS"
	for k in "${key_list[@]}"; do send_key "$k"; done
fi

dead=$(tmux list-panes -t "$SESSION" -F '#{pane_dead}' 2>/dev/null | head -1)

tmux capture-pane -t "$SESSION" -p >"$TXT"
if [ "$SVG" = 1 ]; then
	tmux capture-pane -t "$SESSION" -p -e >"$ANSI"
	if command -v ansisvg >/dev/null 2>&1; then
		ansisvg <"$ANSI" >"$OUT_DIR/$NAME.svg"
	elif command -v go >/dev/null 2>&1; then
		go run github.com/wader/ansisvg@v0.5.0 <"$ANSI" >"$OUT_DIR/$NAME.svg"
	else
		printf '%s\n' "tui-shot: no ansisvg and no go toolchain, skipping $NAME.svg" >&2
	fi
fi

if [ "$dead" = "1" ]; then
	printf '%s\n' "tui-shot: $NAME — the program exited before the capture; screen contents:" >&2
	cat "$TXT" >&2
	exit 1
fi

printf '%s\n' "tui-shot: wrote $TXT"
