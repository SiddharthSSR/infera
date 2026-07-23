# Design-partner request intake

INF-63 adds `/request-access` and intentionally does not hardcode an email
address, form provider, or storage destination. INF-69 keeps the public
acquisition path on the evaluation and quickstart surfaces until an
administrator configures an approved endpoint at frontend build time. When the
endpoint is absent or invalid, `/request-access` shows an informational state
and renders no form or submit control.

## Required administrator input

Set `VITE_DESIGN_PARTNER_REQUEST_ENDPOINT` to either:

- a same-origin path beginning with `/`, such as an administrator-provisioned
  `/api/design-partner-requests`; or
- an absolute `https://` endpoint that accepts cross-origin browser requests.

HTTP and protocol-relative destinations are rejected. The Docker frontend build
accepts the same variable as a build argument, and each Compose configuration
forwards it. Vite embeds build variables in browser assets, so this value must be
a public routing destination—not a secret, token, personal email address, or
credential.

The repository does not contain an approved intake destination. Before enabling
production submission, the administrator must supply the endpoint above and
verify its authentication posture, CORS policy when cross-origin, encryption,
access controls, retention/deletion policy, abuse controls, and incident owner.
No personal address discovered in Git or local configuration should be used.
Once the build receives a valid endpoint, the public acquisition links and
request form activate automatically.

## Request contract

The browser sends an unauthenticated `POST` with `Content-Type:
application/json`, `credentials: omit`, and `referrerPolicy: no-referrer`.
Successful delivery is any 2xx response. The JSON body contains exactly:

```json
{
  "workEmail": "operator@example.com",
  "company": "Example Systems",
  "role": "Infrastructure lead",
  "currentInferenceStack": "OpenAI-compatible client with self-hosted workers",
  "evaluationGoal": "Evaluate migration of one inference route."
}
```

The receiving service must enforce equivalent length and content validation.
It must not expect API keys, credentials, prompts, model output, customer data,
cookies, analytics identifiers, or payment information.

## Privacy-safe measurement

The configured form emits only two fixed-schema analytics events:

- `design_partner_request_started` with `source: request_access`
- `design_partner_request_submitted` with one of `succeeded`,
  `validation_failed`, `delivery_failed`, or `configuration_missing`

No form value is passed to analytics. Use aggregate start-to-success counts as
the funnel measure. Treat validation/delivery/configuration outcomes as
guardrails; do not add identity to join events. The unconfigured informational
state renders no form and emits no design-partner form event.
