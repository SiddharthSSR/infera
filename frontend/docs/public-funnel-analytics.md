# Privacy-safe public funnel analytics

INF-57 defines eight stable, vendor-neutral events. The implementation lives in
`src/lib/publicAnalytics.ts`; its only accepted properties are derived from
`src/types/publicAnalytics.ts`.

## Privacy boundary

The schema uses fixed enum values only. It has no free-form strings, URLs,
referrers, timestamps, user/workspace/session identifiers, or model names. Never
add API keys, prompts, copied code, request/response payloads, invitation tokens,
model output, email addresses, IP addresses, or other identifiers. Runtime
validation rejects the entire event when a caller adds an unknown property or
value. Events and properties are frozen before reaching a transport.

The adapter catches synchronous failures, consumes rejected transport promises,
and returns `void`, so analytics cannot block navigation or inference actions.
First-success events use only the event name in `sessionStorage`; when storage is
unavailable, an in-memory set preserves best-effort deduplication.

No analytics vendor is installed and the repository has no approved analytics
integration. The shipped `publicAnalytics` singleton therefore uses a no-op
transport and performs no network requests. `VITE_PUBLIC_ANALYTICS_ENABLED` is
fail-closed: only the exact value `true` enables dispatch. An approved future
composition root can pass a `PublicAnalyticsTransport` to
`createPublicAnalyticsFromEnv`; the adapter remains responsible for validation.

## Event taxonomy and metrics

| Event | Safe dimensions | Funnel use |
| --- | --- | --- |
| `public_landing_view` | `surface` | Landing denominator |
| `public_primary_cta_clicked` | `action`, `placement` | Landing-to-CTA conversion |
| `public_product_explored` | `product`, `source` | Product-interest mix |
| `public_resource_opened` | `resource`, `source` | Quickstart/docs engagement |
| `public_sign_in_intent` | `source` | Landing-to-sign-in intent |
| `activation_first_model_list_succeeded` | `surface` | First model-list activation |
| `activation_first_unary_inference_succeeded` | `surface` | First unary activation |
| `activation_first_streaming_inference_succeeded` | `surface` | First streaming activation |

Compute conversion rates from aggregate event counts. Guardrails are rejected
event count, transport error count at the provider boundary, and first-success
deduplication rate. Do not add identity merely to join events; use aggregate
sessionless funnels unless a separately reviewed consent and identity design is
approved.

## Exact integration calls for INF-53

Call tracking after the view/action has actually occurred. Do not pass DOM text,
the current URL, query parameters, or spread application state into properties.

```ts
import { publicAnalytics } from '../lib/publicAnalytics';

publicAnalytics.track('public_landing_view', {
  surface: 'migration_landing',
});

publicAnalytics.track('public_primary_cta_clicked', {
  action: 'start_building',
  placement: 'hero', // use 'closing' for the closing CTA
});

publicAnalytics.track('public_product_explored', {
  product: 'model_catalog', // or 'playground' / 'openai_compatibility'
  source: 'landing',
});

publicAnalytics.track('public_resource_opened', {
  resource: 'api_docs', // use 'quickstart' for the quickstart link
  source: 'landing',
});

publicAnalytics.track('public_sign_in_intent', {
  source: 'landing',
});
```

## Exact integration calls for INF-55

Place success calls only after a successful response has been parsed. Never pass
the response, model name, prompt, generated output, API key, or error object.

```ts
import { publicAnalytics } from '../lib/publicAnalytics';

publicAnalytics.track('public_resource_opened', {
  resource: 'quickstart', // use 'api_docs' for docs
  source: 'onboarding',
});

publicAnalytics.track('public_sign_in_intent', {
  source: 'onboarding', // invitation acceptance uses 'invitation'
});

publicAnalytics.trackFirst('activation_first_model_list_succeeded', {
  surface: 'onboarding',
});

publicAnalytics.trackFirst('activation_first_unary_inference_succeeded', {
  surface: 'onboarding',
});

publicAnalytics.trackFirst('activation_first_streaming_inference_succeeded', {
  surface: 'onboarding',
});
```

For events initiated from the public navigation, use
`source: 'public_navigation'`. For first successes in the existing product UI,
use `model_catalog` or `playground` as the typed `surface` value.
