# Findings: does an AgentBrief AGENTS.md help an agent orient itself?

Short answer: in this benchmark, no measurable effect on accuracy, turns, tokens or cost in any of the three
experiments. Every arm scores about 4.8-4.9 out of 5. The benchmark is saturated, so it cannot separate the arms.
It does not show that AgentBrief is useless. It shows that on these repos, with this task, there was nothing
for it to fix.

Task: a read-only Claude Code session answers 5 orientation questions (install, full test, single-file test,
CI lint, generated files) about 8 real repos (4 for drift). Graded 0/0.5/1 per question by a sonnet judge that
is blind to the arm. Deltas are arm minus `none`, with the standard error of the difference in means. n is the
number of runs per arm. Full tables are in `<tag>/summary.md`.

## 1. sonnet (n=24 per arm: 8 repos x 3 reps)
| arm | accuracy /5 | turns | tokens | cost USD |
|---|---|---|---|---|
| none | 4.875 (sd 0.30) | 6.6 (sd 2.8) | 93.6k | 0.067 |
| llm | 4.792 (sd 0.25) | 6.5 (sd 3.2) | 87.8k | 0.064 |
| agentbrief | 4.875 (sd 0.27) | 6.1 (sd 2.8) | 98.8k | 0.066 |

Deltas versus none, accuracy: agentbrief +0.00 (SE 0.08), llm -0.08 (SE 0.08). Turns: agentbrief -0.5 (SE 0.8), llm -0.1 (SE 0.9).
Tokens: agentbrief +5% (SE 8%), llm -6% (SE 8%). All within noise.
The llm arm also carried a one-off generation cost per repo, listed in `sonnet/summary.md` and not included above.

## 2. haiku answering model (n=16 per arm: 8 repos x 2 reps)
| arm | accuracy /5 | turns | tokens | cost USD |
|---|---|---|---|---|
| none | 4.906 (sd 0.20) | 12.3 (sd 3.2) | 147.7k | 0.007 |
| llm | 4.875 (sd 0.22) | 12.7 (sd 2.7) | 143.4k | 0.007 |
| agentbrief | 4.812 (sd 0.36) | 13.2 (sd 3.1) | 161.1k | 0.008 |

Deltas versus none, accuracy: agentbrief -0.09 (SE 0.10), llm -0.03 (SE 0.08). Turns: agentbrief +0.9 (SE 1.1). Tokens: agentbrief +9% (SE 11%). Null again.
The weaker model did not fail more without help, so there was no headroom for a benefit.

## 3. drift (constructed; n=8 per arm: 4 repos x 2 reps)
The `llm-stale` arm uses the LLM file written before a deterministic mutation. The mutations were renamed
scripts/test targets (excalidraw, vite, fastapi), a pnpm bump, and a new CI clippy step plus a renamed test target
(ripgrep). The `agentbrief` arm was re-synced on the mutated repo. This measures the cost of stale
instructions. It says nothing about how often real repos drift, and the mutations were picked by the benchmark author.

| arm | accuracy /5 | turns | tokens | cost USD |
|---|---|---|---|---|
| none | 4.875 (sd 0.35) | 9.1 (sd 3.2) | 104.7k | 0.085 |
| llm-stale | 4.812 (sd 0.26) | 8.4 (sd 4.0) | 111.4k | 0.095 |
| agentbrief | 4.875 (sd 0.23) | 9.1 (sd 2.5) | 105.5k | 0.091 |

Deltas versus none, accuracy: llm-stale -0.06 (SE 0.16), agentbrief 0.00 (SE 0.15). The stale file did not hurt measurably.
Agents opened the manifests anyway, ignoring or correcting the stale lines. The only stale-linked misses were
one fastapi install answer (quoted the old `uv sync --group dev` form) in 1 of 8 runs (a 0.5 on q1); the effect is tiny.
The `agentbrief` arm got the renames right in its synced output (for example `yarn test:unit` and the new clippy line),
but it also fell back to unverified convention commands in some cases, such as `uv run pytest` and `uv run ruff check .` for fastapi.

## Interpretation
- Saturation. These repos have clear READMEs, CONTRIBUTING files and manifests, and read-only exploration finds the
  answers in 3-12 turns. Ceiling effects mean small real benefits (a few percent) cannot be detected at this n.
  Minimum detectable difference is roughly 0.2-0.3 out of 5.
- Most remaining misses are q5 (generated/vendored files), where judging is subjective. Several are agents
  over-claiming generated files. In the agentbrief arm, a few q5 answers listed the AGENTS.md block itself as
  generated or do-not-edit, and the judge penalised it. That is partly a benchmark artifact (the truth does not
  cover the file), but it is also a small real side effect of that line in the output.
- Haiku ripgrep/agentbrief q3 invented a wrong test target (`--test tests`), a unique miss on a single run.
- Token and turn counts show no saving from either file, so the "faster orientation" claim is not supported here.
- Not tested: end-to-end task success, larger or poorly documented repos, write/execute sessions where a wrong
  command costs real time, and long-run staleness. The ETH result that LLM-written files raise cost ~20% was not
  reproduced: llm cost is -5% (SE 11% relative) versus none.
- Reasonable next step: pick harder repos (sparse docs, many scripts) or an execution-based task where wrong commands fail visibly.
