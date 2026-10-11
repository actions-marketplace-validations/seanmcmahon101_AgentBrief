# AGENTS.md

Yarn 1 workspaces monorepo (`yarn@1.22.22`, Node >= 18): `excalidraw-app`, `packages/*`, `examples/*`.

## Commands

Run from the repo root.

- Install: `yarn`
- Dev server (the web app): `yarn start` (Vite, runs in `excalidraw-app`)
- Build app: `yarn build`; build all library packages: `yarn build:packages` (order matters: common → fractional-indexing → laser-pointer → math → element → excalidraw)
- Tests (Vitest, jsdom): `yarn test` (watch mode) or `yarn test:app --watch=false`
- Single file: `yarn test:app --watch=false packages/excalidraw/tests/App.test.tsx`
- Single test by name: `yarn test:app --watch=false -t "test name"`
- Update snapshots: `yarn test:update`
- Typecheck: `yarn test:typecheck` (`tsc`)
- Lint: `yarn test:code` (ESLint, `--max-warnings=0`); autofix `yarn fix:code`
- Format check (css/scss/json/md/html/yml only): `yarn test:other`; fix `yarn fix:other`
- Everything CI-style: `yarn test:all`
- Pre-commit hook (husky + lint-staged) runs ESLint `--fix` on js/ts/tsx and Prettier on the other file types.

## Architecture

- `excalidraw-app/` is the hosted app (excalidraw.com). It wraps the `@excalidraw/excalidraw` component and adds collaboration (`collab/`, socket.io via `Collab.tsx`/`Portal.tsx`), Firebase storage, local persistence and cross-tab sync (`data/`).
- `packages/excalidraw/` is the published React editor component. The editor is the very large class component `components/App.tsx`; it ties together the other pieces below.
- Internal packages are imported as `@excalidraw/{common,element,math,utils,excalidraw,fractional-indexing,laser-pointer}`. They resolve to source via aliases in `tsconfig.json` and `vitest.config.mts`. Adding a new package or alias means updating both, plus the Vite config of the app.
- `packages/element/src/` holds the element model and logic: types, `Scene` (element container), `mutateElement`, binding, arrows, text, frames, resize and so on. `Store` (`store.ts`) captures state deltas (`StoreDelta`/increments) from the scene and appState.
- `packages/excalidraw/history.ts` builds undo/redo (`History`, `HistoryDelta`) on top of those `StoreDelta`s.
- Mutations of editor state go through actions: `packages/excalidraw/actions/` (`register.ts`, `types.ts`, `manager.tsx` with `ActionManager`). Each action returns the new elements/appState, and also handles keyboard shortcuts and UI.
- Rendering is split across two canvases: static scene (`renderer/staticScene.ts`) and interactive overlay (`renderer/interactiveScene.ts`). `scene/Renderer.ts` computes the visible elements, and `scene/export.ts` plus `renderer/staticSvgScene.ts` handle PNG/SVG export.
- State is React `appState` (`appState.ts`) plus Jotai atoms (`editor-jotai.ts` for the library, `excalidraw-app/app-jotai.ts` for the app).
- Tests sit in `packages/*/tests`, `packages/*/__tests__`, `*.test.ts(x)` beside sources, and `excalidraw-app/tests`. Global setup is `setupTests.ts`.
