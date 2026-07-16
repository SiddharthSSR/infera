# AGENTS.md

## Scope

`frontend/` contains the React dashboard, workspace admin views, logs, models, instances, API keys, and the chat/Hermes playground.

## Key Paths

- `src/pages/`: route-level screens
- `src/components/`: shared UI pieces
- `src/components/shared/`: base layout and display primitives
- `src/lib/`: API client, app state, workspace helpers
- `src/hooks/`: reusable hooks
- `src/types/`: frontend API and domain types
- `src/test/setup.ts`: test setup

## Commands

- Install deps: `npm ci`
- Start dev server: `npm run dev`
- Build: `npm run build`
- Lint: `npm run lint`
- Test: `npm run test:run`

## Working Rules

- Preserve the existing dashboard design language; improve and componentize it rather than redesigning it.
- Reuse `src/components/shared/`, `src/lib/api.ts`, and existing hooks/context before adding new patterns.
- Keep frontend API types aligned with backend contracts in `src/types/index.ts`.
- Prefer testing behavior at page/lib level when modifying user-visible flows.
- When changing playground or Hermes UX, verify both the main output state and the run-history/trace behavior.

## Pitfalls

- This app uses Vitest in `jsdom`; keep browser-only assumptions out of shared utilities unless tested.
- `npm run build` currently succeeds with an existing large-chunk Vite warning; do not treat that warning alone as a new regression.
- Many screens have paired desktop/mobile tests; avoid updating one responsive path without checking the other when layout logic changes.

## Validation

- Run `npm run test:run` for touched views or libraries and `npm run build` for user-facing changes.
- Keep accessibility and keyboard behavior intact for new controls.
- Update tests alongside API-shape or state-management changes.
