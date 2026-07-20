# Infera final production UX smoke

Date: 2026-07-20 IST

Production URL: https://inferai.co.in/

## Result

Conditional pass. The public landing, migration quickstart, API docs, trust record, invitation entry, and sign-in entry all render and navigate correctly on desktop and a 390 x 844 mobile viewport. No browser console warnings or errors were observed. Authenticated onboarding and dashboard navigation were not exercised because no credential was entered.

Two follow-ups remain before closing the conversion work:

1. Conversion analytics are defined but not connected. `frontend/src/lib/publicAnalytics.ts` instantiates the production singleton with a no-op transport, and no production call sites invoke `publicAnalytics.track` or `trackFirst`.
2. Reduced-motion handling is incomplete for the landing hero. `frontend/src/index.css` applies the 360 ms `public-hero-enter` animation to hero content, while the reduced-motion override disables animations only under `.docs-page` and resets later landing sections.

## Flow evidence

- Landing desktop: `landing-desktop-live.png`
- Landing mobile top: `landing-mobile-top-390x844.png`
- Landing mobile menu: `landing-mobile-menu-open.png`
- Quickstart desktop: `quickstart-desktop.png`
- API docs desktop: `docs-desktop.png`
- Sign-in desktop: `sign-in-desktop.png`
- Sign-in mobile: `sign-in-mobile-390x844.png`
- Initial stale pre-release Chrome tab, retained as diagnostic evidence: `landing-desktop-service-unavailable.png`

The initial Chrome tab showed an old fail-closed page. A fresh navigation returned the current application and rendered correctly. Independent HTTPS checks confirmed the root app shell and Grafana health returned HTTP 200. Gateway health returned HTTP 200 with `status: degraded` because the approved cost-saving state has zero workers.

## Accessibility and responsive checks

- Skip link and semantic landmarks are present on public pages.
- The sign-in API-key field receives initial focus with a 2 px solid focus indicator.
- Keyboard Tab advances from the key field to the Show API key button.
- Mobile navigation opens and exposes Product, OpenAI Migration, Docs, Trust, GitHub, and Sign In.
- No horizontal overflow at 390 px on landing or sign-in.
- Mobile menu target is 72 x 44 px; primary and secondary CTA targets are 350 x 48 px.
- Sample contrast ratios: hero title 17.18:1, lede 5.59:1, primary CTA 17.02:1, secondary CTA 17.18:1.

## Validation results

`npm run test:run -- src/pages/PublicLanding.test.tsx src/pages/Login.test.tsx src/pages/TrustSurfaces.test.tsx src/lib/publicAnalytics.test.ts`

- 4 test files passed
- 40 tests passed

`npm run build`

- TypeScript and Vite production build passed
- 676 modules transformed

Read-only production checks:

- `/`: HTTP 200, current app shell
- `/health`: HTTP 200, gateway reachable, zero-worker degraded state
- `dashboard.inferai.co.in/api/health`: HTTP 200, database ok
- Browser console errors/warnings across the tested public journey: none

## Recommended release decision

Keep INF-50 open until analytics has a real approved transport and conversion call sites, and the landing hero honors `prefers-reduced-motion`. After those two narrow fixes, repeat this smoke and exercise authenticated onboarding/dashboard navigation with an approved test account.
