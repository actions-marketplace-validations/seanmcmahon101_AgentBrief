# AGENTS.md

pnpm monorepo (pnpm only; Node `^20.19.0 || >=22.12.0`). Packages: `packages/vite` (core), `packages/create-vite`, `packages/plugin-legacy`; integration test apps in `playground/`; docs in `docs/`.

## Commands

Run from the repo root.

- Install: `pnpm i`
- Build all packages: `pnpm build` (must be done before running playground tests)
- Rebuild core on change: `cd packages/vite && pnpm dev`
- Lint: `pnpm lint` (eslint); format: `pnpm format` (oxfmt)
- Typecheck: `pnpm typecheck`
- Unit tests (`**/__tests__/**/*.spec.[tj]s`, excluding `playground/`): `pnpm test-unit`
- Playground integration tests (Vitest + Playwright): `pnpm test-serve` (dev server), `pnpm test-build` (build mode), `pnpm test-serve-bundled` (forces `experimental.bundledDev`)
- All tests: `pnpm test` (unit + serve + build; does not accept a filter)
- Docs build: `pnpm docs-build`

### Running a single test

- Unit: `pnpm test-unit <filename-or-path-filter>`, e.g. `pnpm test-unit packages/vite/src/node/__tests__/config.spec.ts`; add `-t "<test name>"` to narrow to one case.
- Playground: the argument is a filter on spec paths, e.g. `pnpm test-serve assets` (matches `playground/assets` and `playground/assets-sanitize`) or `pnpm test-build assets`.
- Debug playground tests with `pnpm debug-serve <filter>` / `pnpm debug-build <filter>` (see CONTRIBUTING.md).

## Playground test conventions

- Specs live in `playground/<name>/__tests__/*.spec.ts`; they import `page`, `isBuild`, `editFile`, etc. from `~utils` (alias for `playground/test-utils.ts`). Setup is in `playground/vitestSetup.ts` and `vitestGlobalSetup.ts`.
- Playgrounds are copied to `playground-temp/` for each run.
- A spec in a nested `__tests__/<folder>/` uses `vite.config-<folder>.js` from the playground root if present.
- `editFile` throws if an edit leaves the file's byte length unchanged (the watcher polls); make edits change the size.
- Test-build uses different default Vite config than a real production build (see `playground/vitestSetup.ts`).
- Mock deps in playgrounds are named `@vitejs/test-*`; use `file:` to simulate copying into `node_modules`, `link:` for symlinks.
- Specs that fail under bundled dev are listed in `bundledDevExclude` in `vitest.config.e2e.ts`, or use `bundledDevTodo()` / `bundledDevUnsupported()` from `playground/test-utils.ts`.

## Architecture (`packages/vite/src`)

The core is built with rolldown (`packages/vite/rolldown.config.ts`) into `dist/`. It has four source areas with separate tsconfigs:

- `node/`: the Node-side tool (CLI, config, dev server, build, plugins, optimizer, SSR).
- `client/`: browser code injected during dev (HMR client, error overlay).
- `module-runner/`: runtime that evaluates transformed modules outside the browser (SSR and other environments); exported as `vite/module-runner`.
- `shared/`: code used by both `node/` and the runtime sides (HMR, module-runner transport, ssrTransform helpers).

Key flows in `node/`:

- `config.ts` resolves user config into per-environment config. Each environment (`environment.ts`, `baseEnvironment.ts`, `server/environments/`) has its own plugin pipeline, module graph and transform.
- `plugins/index.ts` assembles the built-in plugin list (css, asset, html, resolve, import analysis, etc.). `plugin.ts` defines the Vite plugin API on top of the rolldown plugin API.
- Dev: `server/index.ts` creates the server and middlewares. `server/pluginContainer.ts` runs plugin hooks for unbundled serving, and `server/transformRequest.ts` turns requests into transformed modules via `server/moduleGraph.ts`. `server/hmr.ts` and `server/ws.ts` push updates to `client/`. `server/bundledDev.ts` (with `client/bundledDev*.ts`) is the experimental bundled-dev mode.
- Build: `build.ts` drives rolldown using the same plugin set. `preview.ts` serves the output.
- SSR: `ssr/` (`ssrTransform.ts`, `fetchModule.ts`, `runnerImport.ts`) provides transform and module fetching for `module-runner/`.
- `optimizer/` pre-bundles dependencies for dev.
