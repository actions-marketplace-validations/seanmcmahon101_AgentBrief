# `agentbrief check` on LLM-written AGENTS.md files

Reproduce with `bench/check_llm.sh <agentbrief-binary>` after `run.py` has prepared the `llm` and `drift` arm directories. Output below is verbatim (paths shortened).

- Fresh files (Claude Code, Sonnet, one per repo, 8 repos): **8 of 8 clean**, no false positives.
- Stale files (same files after the deterministic mutations in `drift.py`, 4 repos): **drift caught in 3 of 4**, 10 errors, all genuine. ripgrep is not caught: the renamed `[[test]]` target `integration` still exists in another workspace crate (`crates/matcher`), so `check` gives it the benefit of the doubt. The stale prose "there is no clippy step in CI" is not checkable.

```
== llm cobra
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm excalidraw
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm fastapi
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm httpx
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm ripgrep
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm sinatra
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm spring-petclinic
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== llm vite
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== drift/llm-stale excalidraw
AGENTS.md:12: error: `yarn test:app --watch=false` runs script `test:app`, which is not defined in any package.json [unknown-script]
AGENTS.md:13: error: `yarn test:app --watch=false packages/excalidraw/tests/App.test.tsx` runs script `test:app`, which is not defined in any package.json [unknown-script]
AGENTS.md:14: error: `yarn test:app --watch=false -t "test name"` runs script `test:app`, which is not defined in any package.json [unknown-script]
AGENTS.md:16: error: `yarn test:typecheck` runs script `test:typecheck`, which is not defined in any package.json [unknown-script]
AGENTS.md:17: error: `yarn test:code` runs script `test:code`, which is not defined in any package.json [unknown-script]
AGENTS.md:18: error: `yarn test:other` runs script `test:other`, which is not defined in any package.json [unknown-script]
6 errors, 0 warnings
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== drift/llm-stale fastapi
AGENTS.md:9: error: `bash scripts/test.sh` refers to `scripts/test.sh`, which does not exist in the repository [missing-path]
AGENTS.md:11: error: `bash scripts/lint.sh` refers to `scripts/lint.sh`, which does not exist in the repository [missing-path]
2 errors, 0 warnings
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== drift/llm-stale ripgrep
ok: 2 instruction files checked, no problems found
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
== drift/llm-stale vite
AGENTS.md:14: error: `pnpm test-unit` runs script `test-unit`, which is not defined in any package.json [unknown-script]
AGENTS.md:21: error: `pnpm test-unit <filename-or-path-filter>` runs script `test-unit`, which is not defined in any package.json [unknown-script]
2 errors, 0 warnings
note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one
```
