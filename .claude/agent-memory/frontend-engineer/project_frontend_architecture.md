---
name: Infera Frontend Architecture
description: Full snapshot of the frontend stack, pages, components, design system, state management, and API client as of 2026-03-30
type: project
---

## Stack
- Vite + React 18 + TypeScript (strict, no `any`)
- React Router DOM v7 (client-side SPA, no SSR)
- TanStack Query v5 for all server state (no Zustand/Redux — auth uses React Context + useState in App.tsx)
- Tailwind CSS v4 (via `@import "tailwindcss"`) + custom CSS properties (not utility-only — heavily custom CSS classes)
- Sonner for toast notifications
- Recharts (imported but not yet visible in core pages — available for charts)
- Lucide-react icons (used sparsely — mostly in StatsCards and ProvisionModal)
- react-markdown + remark-gfm + rehype-highlight for Playground responses
- Vitest + Testing Library for unit/component tests

## Design System
- Defined entirely in `frontend/src/index.css` (CSS custom properties + class-based design tokens)
- Color tokens: `--bg-paper: #FDFBF8`, `--bg-accent: #F4F2EE`, `--text-primary: #050505`, `--text-secondary: #555555`, `--text-tertiary: #8a8886`, `--border-color: #D8D6D4`
- Semantic colors: `--color-success: #2E7D32`, `--color-warning: #F9A825`, `--color-error: #C62828`, `--color-info: #1565C0`
- Typography: DM Sans / Inter (sans), Space Mono (mono); nav links are all-caps micro-type (0.65–0.7rem, letter-spacing: 0.15em)
- Display header: `clamp(4.25rem, 8vw, 6.75rem)` all-caps — used on every authenticated page except Playground
- Max-width shell: `min(100%, 1720px)` centered; grid line borders on left/right
- NOT a Tailwind-first project — Tailwind is imported but most styling is bespoke CSS classes. `StatsCards.tsx` is the outlier that uses raw Tailwind utility classes (inconsistent with the rest).

## Pages (11 routed, 3 public/lazy)
| File | Route | Lines | Notes |
|------|-------|-------|-------|
| Dashboard.tsx | / | 1270 | Ops command center; attention queue, usage charts, deployment history, onboarding checklist |
| Instances.tsx | /instances | 1715 | GPU node lifecycle: provision, start/stop/terminate, deployment timeline, inference verification |
| Models.tsx | /models | 1003 | Vault registry + live model list; deploy-from-model flow |
| Playground.tsx | /playground | 532 | Chat interface with streaming; history sidebar; model/temp/max-tokens controls |
| WorkspaceAdmin.tsx | /workspace | 1386 | Members, invites, API keys, provider configs, quota, usage audit |
| ApiKeys.tsx | /api-keys | 525 | Key creation, listing, revocation; role selection |
| Logs.tsx | /logs | 235 | **MOCK DATA ONLY** — generates fake log entries on a timer; not connected to real gateway logs |
| Login.tsx | * (unauthenticated) | 252 | API key login; health check polling |
| GettingStarted.tsx | /getting-started | 309 | Static quickstart guide with code examples |
| PublicApiDocs.tsx | /docs | 333 | Public API reference (no auth required) |
| AcceptInvitation.tsx | /accept-invite | 219 | Token-based workspace invitation acceptance |

Primary nav: DASHBOARD, MODELS, NODES (/instances), PLAYGROUND
Secondary nav: LOGS, API KEYS, SETTINGS (/workspace)

## Components
- `EmptyState.tsx` — generic empty state with optional CTA button
- `Skeleton.tsx` / `SkeletonCell.tsx` — simple pulse animation skeleton
- `ErrorBoundary.tsx` — class component, wraps entire app
- `SectionHeader.tsx` — title + optional subtitle + action slot
- `ActionGroup.tsx` — button group primitive
- `CollapsibleSection.tsx` — accordion-style section
- `MetadataList.tsx` — key/value pair list
- `StatusBadge.tsx` — status badge with tone variants
- `InstanceMobileCard.tsx` — mobile card for an instance row
- `InstancesList.tsx` — desktop table for instances
- `StatsCards.tsx` — 4-metric stats strip (uses raw Tailwind + dark bg colors — visually inconsistent with rest of app)
- `WorkersList.tsx` — worker table
- `ProvisionModal.tsx` — modal for provisioning a new GPU node (uses hardcoded model list, not Vault)
- `CostDisplay.tsx` — cost formatting helpers
- `ChatPlayground.tsx` — chat component (appears to be an older/unused version; Playground.tsx has inline chat)
- `CodeExample.tsx` — syntax-highlighted code block
- `Header.tsx` — appears unused (App.tsx builds its own nav inline)

## State Management
- **No Zustand or Redux** — all state is either TanStack Query (server) or React useState (local/lifted)
- Auth state lives in `AppContent` in App.tsx, distributed via `AuthContext` (lib/auth-context.ts)
- Chat state (messages, history, selectedModel, temperature, maxTokens) is lifted into `AppContent` and distributed via `ChatContext` (lib/chat-context.ts) — persists across route changes
- No URL state management (no useSearchParams outside of Instances page)
- QueryClient config: retry:1, staleTime:2s, refetchInterval:5s (global default)

## API Client
- `frontend/src/lib/api.ts` — plain fetch wrapper with HttpOnly cookie auth
- `authFetch()` — attaches `credentials: 'include'`; dispatches `auth-expired` DOM event on 401
- `API_BASE = ''` — all calls are relative (proxied in dev via Vite, collocated in prod)
- Streaming via async generator (`streamChatCompletion`) with SSE parsing
- Endpoints: /api/workers, /api/stats, /api/instances, /api/offerings, /api/providers, /api/costs, /api/deployments, /api/vault/*, /api/auth/*, /api/audit/usage, /v1/models, /v1/chat/completions

## Hooks (frontend/src/hooks/)
- `useApi.ts` — all TanStack Query hooks: useWorkers, useModels, useStats, useInstances, useOfferings, useProviders, useCosts, useDeploymentAttempts, useProvisionInstance, useTerminateInstance, useStartInstance, useStopInstance, useUpdateDeploymentVerification, useMarkDeploymentAutoVerificationRequested, useVaultModels, useVaultStats, useVaultFamilies, useRegisterVaultModel, useDeleteVaultModel
- `useIsMobile.ts` — MediaQueryList-based breakpoint hook (threshold passed as argument)

## Lib utilities (frontend/src/lib/)
- `deploymentHistory.ts` — summarize/classify deployment attempt records
- `instanceReadiness.ts` — derive readiness state from instance + worker data
- `liveWorkspaceOperations.ts` — active operations list for dashboard
- `workspaceMaturity.ts` — workspace health scoring
- `workspaceActivity.ts` — audit activity items
- `workspaceLifecycle.ts` — member/invite status normalization
- `onboardingChecklist.ts` — first-workspace checklist builder
- `modelRuntimeDrilldown.ts` — per-model runtime status
- `stableWorkers.ts` — stabilize worker list across polls (prevents flicker)
- `lazyWithRetry.ts` — lazy import with retry on chunk load failure
- `auth-context.ts` — AuthContext type + useAuthSession hook
- `chat-context.ts` — ChatContext type + useChat hook
- `utils.ts` — cn() (clsx + tailwind-merge)

## Known Issues / Gaps
- Logs page uses entirely fake/mock data — not wired to any real log endpoint
- StatsCards uses raw Tailwind dark-theme classes inconsistent with the warm paper-tone design system
- ProvisionModal has a hardcoded AVAILABLE_MODELS list — does not pull from Vault
- GPU_VRAM_GB and MODEL_DEPLOYMENT_PRESETS are duplicated in both Instances.tsx and Models.tsx
- No route-level error boundaries (only one global ErrorBoundary at root)
- No loading skeletons on most pages — only SkeletonCell is used in Dashboard
- No virtualization for long lists (Workers, Instances, Vault models)
- refetchInterval on useStats is 2s — may cause excessive network traffic
