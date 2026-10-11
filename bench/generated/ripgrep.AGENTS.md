# AGENTS.md

## Commands

Cargo workspace (edition 2024, MSRV 1.96). The `rg` binary comes from `crates/core/main.rs`.

- Build: `cargo build` (release: `cargo build --release`; PCRE2: `--features pcre2`; indexing: `--features unstable-index`)
- Test everything: `cargo test --all` (the README's documented command)
- Single integration test: `cargo test --test integration <name>` (e.g. `cargo test --test integration misc::`)
- Single crate: `cargo test -p grep-searcher` (or `-p ignore`, `-p globset`, etc.)
- Format check (CI): `cargo fmt --all --check` (`rustfmt.toml`: max_width 79)
- Docs check (CI): `RUSTDOCFLAGS='-D warnings' cargo doc --no-deps --document-private-items --workspace`
- Zsh completion vs `--help` consistency (CI): `ci/test-complete` (needs a built `rg` and zsh)

There is no separate lint step (no clippy config) in CI.

## Architecture

The root `ripgrep` package is a thin CLI over a set of reusable library crates in `crates/`:

- `matcher` (`grep-matcher`): the `Matcher` trait abstracting regex engines.
- `regex` (Rust regex) and `pcre2` (optional): `Matcher` implementations.
- `searcher`: runs a `Matcher` over a haystack and emits results to a `Sink`; handles line/multiline, binary detection, context.
- `printer`: `Sink` implementations (standard, summary, JSON output).
- `ignore`: parallel recursive directory walker honoring gitignore/ignore rules and type filters; uses `globset`.
- `cli`: shared CLI helpers. `grep`: facade re-exporting matcher/regex/pcre2/printer/searcher/cli.
- `index` (`grep-index`): experimental, only built with the `unstable-index` feature.

`crates/core` (the `rg` binary) is the glue:
- `flags/`: `parse.rs` (lexopt) → `lowargs.rs` (raw parsed flags) → `hiargs.rs` (`HiArgs`, validated/derived config that builds the walker, matcher, searcher and printer). `defs.rs` defines every flag, and its docs feed `--help`, the man page, and shell completions (`flags/doc`, `flags/complete`). `config.rs` reads the config file.
- `search.rs` / `haystack.rs`: per-file search execution wiring searcher + matcher + printer.
- `index/`: `enabled.rs` vs `disabled.rs` selected by the `unstable-index` feature.

When adding or changing a flag, update `flags/defs.rs`; docs and completions are generated from it.

## Tests

Integration tests live in `tests/` (single `integration` target via `tests/tests.rs`, `autotests = false`). They use `tests/util.rs` (`Dir`/`TestCommand`, via `setup()`/`setup_pcre2()`) to create a temp dir and run the built `rg`. New feature tests go in `tests/feature.rs`; regression tests in `tests/regression.rs`; `tests/misc.rs` is meant not to grow. `tests/index` is compiled only with `unstable-index`. Sub-crates have their own unit tests (some with `tests/` dirs, e.g. `crates/ignore`, `crates/matcher`).

## Project policy

`AI_POLICY.md`: autonomous agents are not allowed for contributing to this project, and AI-generated comments to maintainers are not accepted; read it before preparing contributions.
