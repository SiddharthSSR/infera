# INF-57 / INF-58 follow-up validation

Date: 2026-07-20 IST

Branch: `task/inf-57-inf-58-smoke-followups`

This validates the two gaps found in the production UX smoke. The changes remain branch-only until review and deployment.

## INF-57: bounded first-party funnel analytics

- Added a same-origin browser transport at `POST /api/public-analytics/events`.
- Production frontend container builds enable the transport by default through `VITE_PUBLIC_ANALYTICS_ENABLED=true`; deployments can explicitly override it.
- The gateway accepts only the eight named events and their fixed allowlisted values.
- Unknown fields, unexpected property counts, free-form values, sensitive-looking values, trailing JSON, and bodies larger than 4 KiB are rejected.
- Accepted events increment `infera_gateway_public_funnel_events_total{event,source,target}`. Every label value is derived from the bounded schema.
- Browser requests omit credentials, contain no user/workspace/model/prompt identifiers, and fail without interrupting navigation.
- Call sites cover landing views, primary CTAs, public resources, product exploration, sign-in form and invitation intent, first successful model listing, first successful unary inference, and first successful streaming inference.
- First-success events are session-deduplicated.

## INF-58: reduced motion and responsive smoke

- The existing current-theme motion is preserved for users who have not requested reduced motion.
- Under `prefers-reduced-motion: reduce`, all landing descendants and pseudo-elements use 0.01 ms animation/transition durations, no delay, and one iteration.
- Chrome at 390 x 844 reported `clientWidth=390` and `scrollWidth=390`.
- The mobile navigation changed from `MENU` to an expanded `CLOSE` state and back.
- The hero `Run the quickstart` link navigated to `/getting-started`, where the expected page heading rendered.
- No browser console warnings or errors were observed during the landing-to-quickstart check.

The local Vite server did not run the gateway, so its background session probe logged an expected connection refusal in the terminal. This did not affect the public journey. Gateway behavior is covered by the Go test suite.

## Automated validation

- `go test ./internal/gateway` — passed.
- `go test -ldflags=-linkmode=external ./...` — passed all Go packages. The external linker is required on this machine because Go 1.22.4's internal linker produces `missing LC_UUID load command` test binaries on the installed macOS version.
- `npm test -- --run` — 54 files and 269 tests passed.
- `npm run lint` — passed.
- `npm run build` — passed; 678 modules transformed.
- `docker compose -f docker-compose.prod.yml config --quiet` with non-secret required placeholders — passed. Optional provider token warnings are expected.
- `git diff --check` — passed.

## Remaining release validation

After merge and deployment, repeat the production public smoke, confirm analytics requests return HTTP 204, confirm the Prometheus counter increments, and exercise authenticated onboarding/dashboard activation with an approved test account.
