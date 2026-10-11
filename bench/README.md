# AgentBrief orientation benchmark

Question: does a small block of verified repo facts (AgentBrief's `AGENTS.md`) help a coding agent
orient itself in an unfamiliar repo, compared with no instruction file or an LLM-written one?

## Method
- 8 repos (excalidraw, vite, httpx, fastapi, cobra, ripgrep, spring-petclinic, sinatra), shallow clones in
  `/home/sm/Work/agentbrief-corpus` (read-only; nothing is ever built or executed).
- 5 questions per repo (`tasks/<repo>.json`): package manager + install, full test command, single-file
  test command, CI lint/format commands, generated/vendored files. Ground truth was written by hand from
  manifests, lockfiles, task runners and CI workflows, before looking at any AgentBrief output. Each
  question has `truth`, `accept` criteria, `reject` patterns (some marked "= 0.5") and `evidence` files.
- Arms: `none` (all instruction files and .git removed), `llm` (AGENTS.md written once per repo by Claude
  Code/sonnet from an /init-style prompt, cached in `generated/`), `agentbrief` (output of
  `agentbrief sync` on the stripped copy). For llm/agentbrief, `CLAUDE.md` is just `@AGENTS.md`.
- Each run: fresh copy under `/tmp/...` (never under /home/sm, because Claude Code loads ancestor
  CLAUDE.md files), `claude -p` with read-only tools (Read, Glob, Grep, ls/find/cat/head/grep), sonnet,
  max 30 turns, all five questions in one session. Raw JSON goes to `results/raw/` (gitignored).
- Grading (`grade.py`): an LLM judge (sonnet, no tools) sees question, ground truth and answer only, blind
  to the arm, scoring 0/0.5/1 per question. Grades are cached in `results/grades/`.
- `report.py` writes `results/summary.md`: accuracy /5, turns, tokens (input + cache read + cache creation +
  output), cost, duration as mean (sd, min-max); overall, per repo, per question, every run, plus the llm
  arm's one-off generation cost (reported separately, not folded into per-run cost).

## Run
```
python3 bench/run.py --agentbrief /path/to/agentbrief --jobs 4      # resumable; skips existing results
python3 bench/grade.py --jobs 4
python3 bench/report.py
```
Results live in `results/<tag>/{raw,grades,summary.md}`; pass the same `--tag` to run.py, grade.py and report.py
(default `sonnet`). `generated/` (LLM AGENTS.md files, generated once with sonnet) is shared by all tags, so
arms differ only in the file, not in who wrote it. Flags: `--repos a,b`, `--arms none,llm`, `--reps N` (default 2), `--model`, `--setup-only`.
Delete a file in `results/raw/` to rerun it; `grade.py --force` regrades.

## Experiments
- `sonnet` (default tag): the main run, `--model sonnet`.
- `haiku`: same design with `--model haiku` as the answering model (judge stays sonnet).
  `python3 bench/run.py --tag haiku --model haiku --agentbrief BIN`
- `drift`: a CONSTRUCTED experiment measuring the cost of stale instructions, NOT how often real repos drift.
  `python3 bench/run.py --tag drift --drift --agentbrief BIN`. For excalidraw, vite, fastapi and ripgrep,
  `drift.py` applies a deterministic, asserted mutation to the stripped copy (renamed scripts/test targets,
  a pnpm bump, a renamed script file with CI updated, a new CI clippy step; see the docstrings). Arms:
  `llm-stale` (the LLM AGENTS.md written from the UNmutated repo), `agentbrief` (`sync` run on the mutated
  copy, i.e. what a repo running `agentbrief check`/`sync` in CI would have) and `none`. Ground truth for
  the mutated repos is hand-derived in `tasks/drift/`. Prose docs (README/CONTRIBUTING) are not updated by
  the mutations, which is a choice that hurts `none` and any arm that reads docs. The mutations were chosen
  by the benchmark author knowing what the LLM file says, so results bound the effect of stale facts from
  above; they say nothing about drift frequency.

## What this measures
Orientation accuracy and cost when an agent may only read files. It does NOT measure end-to-end task
success (editing code, passing tests). Nothing is executed, so a command that looks right but fails
at runtime is not detected.

## Threats to validity
- Small N (8 repos x 2 reps per arm): differences under about 0.5/5 are likely noise; sd is reported.
- The LLM judge is non-deterministic (no temperature control) and may share biases with the answerer.
  Criteria are explicit; spot-check the reasons in `results/grades/*.json`.
- Ground truth is one engineer's reading; several questions admit multiple valid answers (see `accept`).
- Ceiling effects: well-documented repos let the `none` arm score high; benefits may show as fewer
  turns/tokens rather than accuracy.
- The llm arm is one generated file per repo; another generation could differ.
- User-level settings/memory (`~/.claude`) still load (`--setting-sources user`); identical across arms.
- Popular repos are probably in the model's training data, which lowers every arm's error.
- Q3 files were chosen to include traps (monorepo sub-package, Go has no per-file tests, ripgrep's single
  integration binary, sinatra-contrib's separate RSpec Gemfile). This is deliberate, not representative.
- Some corpus repos ship their own AGENTS.md/CLAUDE.md (e.g. excalidraw); these are stripped in all arms.
