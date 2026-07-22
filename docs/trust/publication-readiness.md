# Public trust publication decision record

Last repository review: 22 July 2026

This record separates facts supported by the public repository from information that still needs
an administrator or legal owner to approve. It is not a privacy policy, terms of service, security
policy, service-level agreement, or representation about a particular deployment.

## Public evidence available now

| Area | Repository evidence | Boundary |
| --- | --- | --- |
| Source | The source repository and contribution steps are public. | Public source alone does not establish reuse rights. |
| API access | The compatibility guide documents workspace-scoped Bearer tokens and distinguishes machine credentials from browser sessions. | Operators remain responsible for credential issuance, storage, and rotation in their deployment. |
| Provider credentials | Production configuration requires a 32-byte encryption key; the implementation uses authenticated encryption for persisted provider credentials. | Repository configuration is not proof that a specific deployment is configured correctly. |
| Request audit data | The audit schema records request metadata, token counts, latency, status, and a prompt hash rather than a raw prompt field. | No public retention or deletion schedule has been approved. |
| Recovery | Public runbooks cover deployment recovery and shared-ledger backup, restore, cutover, and rollback. | These runbooks do not create an SLA or recovery-time commitment. |
| Ingress | The production nginx configuration includes security headers. | A checked-in configuration does not prove the headers served by every deployment. |

## Administrator decisions required before publication

### 1. Repository license

The root README previously named MIT, while `python/pyproject.toml` names Apache-2.0 and includes
the Apache license classifier. Before adding a root `LICENSE`, the repository owner must approve:

1. whether the repository is MIT, Apache-2.0, or intentionally uses different licenses by path;
2. the scope of each license and any per-directory notices;
3. the copyright holder text and applicable years; and
4. the matching README and package-metadata wording.

### 2. Private security reporting

Before adding `SECURITY.md`, the administrator must provide and approve:

1. a non-public reporting address or intake URL owned by the project;
2. the person or team responsible for monitoring it;
3. supported versions or scope;
4. an acknowledgement target and disclosure expectations; and
5. the public wording that can be maintained operationally.

Until then, do not report vulnerabilities, credentials, secrets, or personal data in public GitHub
issues. The absence of a private channel is a blocker, not permission to use a public channel.

### 3. Company and design-partner contact

The administrator must approve the exact legal or trading name, jurisdiction, publishable address
(if any), team or founder details, a monitored general contact, and a monitored design-partner
intake path. A source-code author name or commit email is not treated as company approval.

### 4. Privacy and data handling

An authorized owner must define the service and deployment scope, organizational roles, data
categories, purposes, subprocessors, transfer locations, retention periods, deletion behavior,
request channel, effective date, and change-notice process. Repository implementation details must
not be promoted into a privacy promise without that review.

### 5. Terms, service status, and commitments

An authorized owner must approve applicable terms, service scope, support boundaries, governing
terms, any service-level commitments, and the owner and URL of a public status page. No uptime,
response-time, customer, certification, or audit claim should be published from repository code
alone.

## Publication rule

Public pages may link to repository evidence and label an item as unavailable or awaiting an owner
decision. They must not infer legal identity, contact details, customers, metrics, certifications,
deployment configuration, or contractual commitments from source code.
