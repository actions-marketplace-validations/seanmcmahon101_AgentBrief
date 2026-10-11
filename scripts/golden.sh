#!/bin/sh
# Golden-output regression harness. Usage: scripts/golden.sh update|diff
# Corpus: $AGENTBRIEF_CORPUS (default ~/agentbrief-corpus); see bench/corpus.sh.
# Only the built binary is run; nothing inside the corpus repos is executed.
set -eu
mode=${1:-diff}
root=$(cd "$(dirname "$0")/.." && pwd)
corpus=${AGENTBRIEF_CORPUS:-$HOME/agentbrief-corpus}
[ -d "$corpus" ] || { echo "corpus not found: $corpus (run bench/corpus.sh)" >&2; exit 2; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
bin=$tmp/agentbrief
(cd "$root" && go build -trimpath -o "$bin" ./cmd/agentbrief)

# scoped paths: repo:path (name -> scoped-<n>.md in order)
scopes="vite:packages/vite codex:codex-rs/tui excalidraw:packages/excalidraw ruff:crates/ruff"

gen() { # outdir
	out=$1
	for d in "$corpus"/*/; do
		r=$(basename "$d")
		o=$out/$r
		mkdir -p "$o"
		"$bin" -C "$d" >"$o/brief.md"
		"$bin" -C "$d" --json >"$o/brief.json"
		rc=0
		"$bin" check -C "$d" >"$o/check.txt" 2>&1 || rc=$?
		echo "exit code: $rc" >>"$o/check.txt"
		n=0
		for s in $scopes; do
			[ "${s%%:*}" = "$r" ] || continue
			n=$((n + 1))
			"$bin" -C "$d" brief "${s#*:}" >"$o/scoped-$n.md"
		done
	done
}

case $mode in
update)
	rm -rf "$root/testdata/golden"
	gen "$root/testdata/golden"
	echo "updated testdata/golden"
	;;
diff)
	gen "$tmp/out"
	if diff -ru "$root/testdata/golden" "$tmp/out"; then
		echo "golden: no differences"
	else
		echo "golden: DIFFERENCES (intentional? run scripts/golden.sh update)" >&2
		exit 1
	fi
	;;
*)
	echo "usage: $0 update|diff" >&2
	exit 2
	;;
esac
