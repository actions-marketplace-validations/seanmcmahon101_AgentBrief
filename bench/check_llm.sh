#!/bin/sh
# Run `agentbrief check` on the arm directories prepared by run.py:
#   - llm/<repo>:             fresh AGENTS.md written by Claude Code (expect: clean)
#   - drift/llm-stale/<repo>: the same file after the repo was mutated (expect: drift caught)
# Usage: bench/check_llm.sh /path/to/agentbrief [work-dir]
set -eu
bin=$1
work=${2:-${AGENTBRIEF_BENCH_WORK:-/tmp/agentbrief-bench}}
for arm in llm drift/llm-stale; do
	for dir in "$work/$arm"/*/; do
		printf '== %s %s\n' "$arm" "$(basename "$dir")"
		"$bin" check -C "$dir" || true
	done
done
