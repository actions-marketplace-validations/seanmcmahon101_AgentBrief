# Public repositories where explicit workflows help

These examples were reproduced on pinned snapshots on 2026-10-12. The YAML
files here are **example policies authored for this experiment**, derived from
the linked upstream sources. They are not upstream adoption, endorsements, or
complete replacement CI configurations. Their path mappings are explicit policy
choices; AgentBrief does not discover those mappings automatically.

The measured outcome is deterministic check selection and detection of planted
command drift. No Vite, Ruff or FastAPI commands were executed, and no frontier
agent time/success improvements were measured.

## Vite: integration modes and a build prerequisite

Pinned revision:
[3b72997](https://github.com/vitejs/vite/tree/3b72997d42e7dd2b31898d0e68461e3025e836b6).

The [contribution guide](https://github.com/vitejs/vite/blob/3b72997d42e7dd2b31898d0e68461e3025e836b6/CONTRIBUTING.md)
requires building before integration tests. Its
[root scripts](https://github.com/vitejs/vite/blob/3b72997d42e7dd2b31898d0e68461e3025e836b6/package.json)
separate unit, serve-mode, build-mode and documentation checks.

With [vite.yaml](vite.yaml) installed as `.agentbrief.yaml`:

```sh
agentbrief plan playground/assets/__tests__/assets.spec.ts --json --strict
# build -> integration-build + integration-serve; build appears once

agentbrief plan docs/guide/api-plugin.md --json --strict
# build -> docs
```

AB preserves the explicit preparation step and separates integration modes from
the documentation check. A generic single-test command does not express those
choices. Renaming `test-serve` in the disposable snapshot produces
`unknown-script`, and AB refuses the resulting plan.

This example intentionally includes both integration modes for changes under
`playground/`; it does not prove every such change requires both modes.

## Ruff: generation and different Rust/documentation workflows

Pinned revision:
[eb625e4](https://github.com/astral-sh/ruff/tree/eb625e43e489bc090aae9c4aa4a407cf389f65d0).

Ruff's [contribution guide](https://github.com/astral-sh/ruff/blob/eb625e43e489bc090aae9c4aa4a407cf389f65d0/CONTRIBUTING.md)
documents rule tests and regeneration with `cargo dev generate-all`.
Its [CI workflow](https://github.com/astral-sh/ruff/blob/eb625e43e489bc090aae9c4aa4a407cf389f65d0/.github/workflows/ci.yaml)
prepares README content and generates documentation before a strict MkDocs build.

With [ruff.yaml](ruff.yaml):

```sh
agentbrief plan crates/ruff_linter/src/codes.rs --json --strict
# rule-tests -> rule-generation

agentbrief plan docs/configuration.md --json --strict
# docs-readme -> docs-generate -> docs
# Also reports that docs/configuration.md is declared generated.
```

AB retains these separate workflows and flags a generated document for review.
This example's Rust path rule deliberately covers the entire linter crate; it
does not establish which tests exercise a particular lint rule. Removing
`scripts/generate_mkdocs.py` in the disposable snapshot produces
`missing-path`, and AB refuses the plan.

Ruff's CI has additional documentation checks and broader test matrices. This
example is intentionally partial, including the generation/build sequence.

## FastAPI: the test wrapper owns important setup

Pinned revision:
[f5c6e9b](https://github.com/fastapi/fastapi/tree/f5c6e9b4f9cadc1bf0f9a1fd672e621206b63007).

The actual [test wrapper](https://github.com/fastapi/fastapi/blob/f5c6e9b4f9cadc1bf0f9a1fd672e621206b63007/scripts/test.sh)
sets `PYTHONPATH=./docs_src` and includes both `tests` and `scripts/tests/`.
Choosing a bare pytest invocation loses that explicit setup.

With [fastapi.yaml](fastapi.yaml):

```sh
agentbrief plan fastapi/applications.py --json --strict
agentbrief plan docs_src/query_params/tutorial001_py310.py --json --strict
# Both select: bash scripts/test.sh, from the repository root.
```

The policy also selects the wrapper in the ordinary brief. AB preserves it
without needing to understand or reproduce its shell body. Removing
`scripts/test.sh` in the disposable snapshot produces `missing-path`, and
AB refuses the plan.

## Reproduce and inspect the results

Six path-selection scenarios and three planted drift scenarios are recorded in
[results.json](results.json). [fixtures.json](fixtures.json) pins revisions,
source-file SHA-256 digests (normalized LF), input paths and expected step ids.

From the AgentBrief checkout, with Go and Python 3 available:

```sh
bench/corpus.sh /path/to/corpus
go build -o /path/to/agentbrief ./cmd/agentbrief
python3 scripts/case_studies.py --binary /path/to/agentbrief --corpus /path/to/corpus
```

The runner verifies evidence-file digests, copies each repository into a
temporary snapshot, adds the example policy, checks the selected plans, then
plants drift and checks that plans are refused. It runs only the supplied
AgentBrief binary; it never executes a corpus command or invokes Git. Source
checkouts remain unchanged. Heavy build/dependency directories and symlinks are
omitted from copies, as they are from normal scans.

The examples demonstrate a useful operational contract. They do not establish
upstream approval of these exact mappings, runtime readiness, comprehensive
test coverage, or a benefit over a frontier agent reading the same guides.
