# Catching drift: `agentbrief check` on excalidraw

Upstream: <https://github.com/excalidraw/excalidraw>. Generated from a shallow clone on 2026-10-11. Excalidraw's own `CLAUDE.md` tells agents to run `yarn test:typecheck`. To simulate a rename, one line of `package.json` was changed in a scratch copy: the script `test:typecheck` became `typecheck`. Nothing else was touched.

## Before the rename

```console
$ agentbrief check
CLAUDE.md:1: warning: CLAUDE.md does not import AGENTS.md, so Claude Code will not read it [agents-md-not-loaded]
  add `@AGENTS.md` as the first line (`agentbrief sync` does this)
0 errors, 1 warning
$ echo $?
0
```

(The warning exists upstream: `CLAUDE.md` does not import `AGENTS.md`, so Claude Code ignores `AGENTS.md`. `agentbrief sync` would add the `@AGENTS.md` line.)

## After the rename

```console
$ sed -i 's/"test:typecheck": "tsc"/"typecheck": "tsc"/' package.json
$ agentbrief check
CLAUDE.md:1: warning: CLAUDE.md does not import AGENTS.md, so Claude Code will not read it [agents-md-not-loaded]
  add `@AGENTS.md` as the first line (`agentbrief sync` does this)
CLAUDE.md:17: error: `yarn test:typecheck` runs script `test:typecheck`, which is not defined in any package.json [unknown-script]
  did you mean `yarn typecheck`?
CLAUDE.md:22: error: `yarn test:typecheck` runs script `test:typecheck`, which is not defined in any package.json [unknown-script]
  did you mean `yarn typecheck`?
2 errors, 1 warning
$ echo $?
1
```

Each error carries the file, line, rule id and a suggestion. With `--json` the same findings are emitted under the schema `agentbrief/check/v1`.
