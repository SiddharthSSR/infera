# Provider Conformance

Every provider implementation must satisfy the shared `providers.Provider`
contract before it is considered production-ready.

## Required Behavior

- `Provision` returns a non-nil instance with:
  - stable `provider`
  - non-empty `provider_id`
- `GetInstance` can retrieve a provisioned instance by provider-native ID
- `ListInstances` includes newly provisioned instances
- `Stop` transitions an instance into a stopped or stopping state
- `Start` transitions an instance back toward running
- `Terminate` either:
  - returns a terminated instance on later lookup, or
  - returns a `ProviderError` with code `not_found`
- `ListOfferings` returns provider-labeled offerings
- `GetStatus` returns provider-labeled status
- `WaitForReady` returns success when the instance is usable, or a typed provider error

## Error Contract

Provider implementations should return `*providers.ProviderError` for
provider-originated failures and set:

- `Provider`
- `Code`
- `Message`

The following codes are especially important because other layers use them for
retry behavior and UX:

- `not_found`
- `rate_limited`
- `service_unavailable`
- `timeout`

## Current Coverage

The shared conformance suite currently runs against the `mock` provider and
acts as the baseline for future providers.

`runpod` also has direct tests for:

- constructor validation
- GraphQL error mapping
- not-found handling
- provision request shaping

Before adding another provider, wire it into the shared conformance suite or
add an equivalent adapter-specific contract harness.
