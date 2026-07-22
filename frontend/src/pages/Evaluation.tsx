import { Link } from 'react-router-dom';
import { AppShell, LabelText, PublicFooter, PublicNav } from '../components/shared';
import { publicEvidenceLinks } from '../lib/publicEvidence';

const deploymentPaths = [
  {
    label: 'LOCAL / MAKE',
    title: 'Gateway, frontend, and a mock or connected worker',
    copy: 'Install the Go, Python, and Node dependencies, then run the gateway and frontend from separate terminals. The repository includes a mock provider and a connected mock-worker path for evaluation without provisioning a GPU.',
    evidence: publicEvidenceLinks.readme,
  },
  {
    label: 'LOCAL / COMPOSE',
    title: 'Gateway and frontend in Docker Compose',
    copy: 'The development Compose file builds the gateway and frontend, mounts ./data into the gateway, and exposes ports 8080 and 3000. Provider keys remain optional environment inputs.',
    evidence: publicEvidenceLinks.localCompose,
  },
  {
    label: 'PRODUCTION / COMPOSE',
    title: 'Pinned images, Caddy, PostgreSQL control state, and observability',
    copy: 'The production path requires pinned gateway and worker images, shared PostgreSQL control state, provider-credential encryption, release identity, worker protocol identity, and alerting configuration.',
    evidence: publicEvidenceLinks.productionCompose,
  },
];

const evaluationSteps = [
  {
    time: '00–05',
    title: 'Choose the safe path',
    copy: 'Use the mock provider for a software-only pass. Use RunPod or Vast.ai only when you are ready to supply that provider’s credentials and accept provider-side resource creation.',
  },
  {
    time: '05–12',
    title: 'Boot the control plane',
    copy: 'Start with the README prerequisites and commands. Confirm the gateway health endpoint and frontend before adding a real worker.',
  },
  {
    time: '12–18',
    title: 'Prove discovery and auth',
    copy: 'Create or obtain a workspace-scoped key, call GET /v1/models, and use a returned model id as the request source of truth.',
  },
  {
    time: '18–24',
    title: 'Send one unary request',
    copy: 'Call POST /v1/chat/completions without streaming first. This isolates authentication, model routing, and worker readiness from SSE transport behavior.',
  },
  {
    time: '24–30',
    title: 'Inspect the operator loop',
    copy: 'Check models, nodes, logs, workspace provider state, and API-key controls. Then record which production prerequisites and recovery expectations remain open for your environment.',
  },
];

const comparisonRows = [
  {
    criterion: 'Deployment ownership',
    openai: 'External service boundary; this repository does not document its current operating contract.',
    engine: 'You assemble and operate the service around the engine process.',
    hosted: 'Varies by vendor. Verify the current service contract externally.',
    infera: 'Self-hosted paths are checked in. Your team operates the gateway, frontend, state, ingress, observability, and provider-backed workers.',
  },
  {
    criterion: 'Compatibility surface',
    openai: 'Verify against current OpenAI documentation; this repository does not restate that surface.',
    engine: 'Engine-specific unless you add a compatibility layer.',
    hosted: 'Varies by vendor. Verify endpoints and request fields externally.',
    infera: 'GET /v1/models and POST /v1/chat/completions, including SSE streaming. Legacy completions and embeddings are not exposed.',
  },
  {
    criterion: 'Operator visibility',
    openai: 'Not evaluated by repository evidence.',
    engine: 'Engine-level behavior; the surrounding operational view is yours to assemble.',
    hosted: 'Not evaluated by repository evidence.',
    infera: 'Dashboard surfaces models, nodes, logs, and workspace controls; production Compose includes Prometheus, Alertmanager, and Grafana.',
  },
  {
    criterion: 'Provider orchestration',
    openai: 'Not part of the deployment path documented here.',
    engine: 'Outside the engine layer in this repository architecture.',
    hosted: 'Not evaluated by repository evidence.',
    infera: 'Gateway adapters exist for RunPod and Vast.ai. Adapter capabilities and errors are exposed through a shared provider contract.',
  },
  {
    criterion: 'Model discovery',
    openai: 'Verify the current provider contract externally.',
    engine: 'Depends on the engine and the service you build around it.',
    hosted: 'Verify the current vendor contract externally.',
    infera: 'The model list merges worker-loaded models with vault metadata and may return safe operator fields beyond the OpenAI core shape.',
  },
  {
    criterion: 'Logs and workspace controls',
    openai: 'Not evaluated by repository evidence.',
    engine: 'Not supplied by an engine alone in the architecture documented here.',
    hosted: 'Not evaluated by repository evidence.',
    infera: 'The application includes logs, workspace membership/provider settings, and workspace-scoped human and service-account access paths.',
  },
];

const faqs = [
  {
    question: 'Is Infera self-hosted or managed?',
    answer: 'This repository documents self-hosted local and production Compose deployments. It does not contain a managed-service availability or support contract, so do not infer one from the public site.',
    evidence: publicEvidenceLinks.readme,
  },
  {
    question: 'Which public inference endpoints exist?',
    answer: 'The documented public surface is GET /v1/models and POST /v1/chat/completions, with unary and SSE streaming responses. Legacy completions and embeddings are current limitations.',
    evidence: publicEvidenceLinks.compatibility,
  },
  {
    question: 'How do keys and workspaces relate?',
    answer: 'Public API calls use workspace-scoped Bearer tokens. Browser sessions are a separate human-auth path, and service-account keys are the documented choice for unattended automation.',
    evidence: publicEvidenceLinks.compatibility,
  },
  {
    question: 'Does a catalog entry mean a model is ready?',
    answer: 'No. The model catalog combines vault metadata with worker-loaded models. A request still needs a healthy worker serving the selected model; use the live /v1/models response as the request source of truth.',
    evidence: publicEvidenceLinks.compatibility,
  },
  {
    question: 'Which providers and engines are implemented?',
    answer: 'The gateway contains RunPod and Vast.ai provider adapters. Worker configuration recognizes vLLM, SGLang, TensorRT-LLM, and mock engines; optional engines require their runtime dependencies and reviewed worker images.',
    evidence: publicEvidenceLinks.modularBackend,
  },
  {
    question: 'What hardware should an evaluation use?',
    answer: 'There is no repository-wide minimum GPU claim. Hardware must fit the selected model and engine configuration. Live model metadata may include vram_required; the production recovery runbook lists only the GPU values reviewed for that RunPod adapter path.',
    evidence: publicEvidenceLinks.deploymentRecovery,
  },
  {
    question: 'What must be persisted and backed up?',
    answer: 'Production requires shared PostgreSQL control state. Multi-replica audit and quota state also requires PostgreSQL; SQLite is single-replica only. Back up database state and the provider-credential encryption key separately—losing that key makes stored provider credentials unrecoverable.',
    evidence: publicEvidenceLinks.sharedAuditLedger,
  },
  {
    question: 'What should block a production decision?',
    answer: 'Unpinned images, missing production secrets, an unavailable control-state database, incompatible gateway and worker release identities, or an untested backup and recovery path are explicit blockers in the checked-in production guidance.',
    evidence: publicEvidenceLinks.deploymentRecovery,
  },
];

function EvidenceLink({ href }: { href: string }) {
  return (
    <a className="evaluation-evidence-link" href={href} target="_blank" rel="noreferrer">
      Repository evidence<span className="sr-only"> (opens in a new tab)</span>
    </a>
  );
}

export function Evaluation() {
  return (
    <AppShell variant="public">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="EVALUATION GUIDE" />

      <main id="main-content">
        <section className="docs-hero evaluation-hero">
          <div className="docs-kicker">Buyer and operator evaluation</div>
          <div className="docs-hero-grid">
            <div>
              <h1 className="docs-title">Decide with the repository open.</h1>
              <p className="docs-subtitle">
                Use this source-backed guide to test the current API, deployment, provider, and operating boundaries. Every Infera claim links to checked-in evidence; third-party capabilities are left for their owners to document.
              </p>
              <div className="docs-hero-strip" aria-label="Evaluation principles">
                <span className="docs-pill">No benchmark claims</span>
                <span className="docs-pill">No managed-service promise</span>
                <span className="docs-pill">Current limits included</span>
              </div>
              <div className="docs-actions">
                <a className="btn-primary docs-action-link" href="#plan">RUN THE 30-MINUTE PLAN</a>
                <Link className="btn-quiet" to="/getting-started">OPEN QUICKSTART</Link>
              </div>
            </div>
            <div className="evaluation-decision-card" aria-labelledby="decision-card-title">
              <LabelText as="div">DECISION FRAME</LabelText>
              <h2 id="decision-card-title">Fit is a boundary check.</h2>
              <dl>
                <div><dt>Client</dt><dd>Can the current chat surface serve your request shape?</dd></div>
                <div><dt>Runtime</dt><dd>Can a reviewed engine and model run on your chosen hardware?</dd></div>
                <div><dt>Operations</dt><dd>Can your team own state, credentials, observability, and recovery?</dd></div>
              </dl>
            </div>
          </div>
        </section>

        <div className="docs-layout">
          <aside className="docs-sidebar">
            <LabelText as="div">ON THIS PAGE</LabelText>
            <nav className="docs-sidebar-nav" aria-label="On this page">
              <a className="docs-sidebar-link" href="#deployment">Deployment paths</a>
              <a className="docs-sidebar-link" href="#prerequisites">Production prerequisites</a>
              <a className="docs-sidebar-link" href="#plan">30-minute plan</a>
              <a className="docs-sidebar-link" href="#comparison">Factual comparison</a>
              <a className="docs-sidebar-link" href="#faq">Buyer FAQ</a>
            </nav>
            <div className="docs-sidebar-card">
              <LabelText as="div">EVIDENCE RULE</LabelText>
              <p className="evaluation-sidebar-copy">“Not evaluated” means this repository cannot support the claim. It does not mean the external product lacks the capability.</p>
            </div>
          </aside>

          <div className="docs-main">
            <section className="docs-section" id="deployment">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">IMPLEMENTED PATHS</LabelText>
                  <h2 className="docs-section-title">Choose the smallest environment that answers your question.</h2>
                </div>
                <p className="docs-section-copy">No Kubernetes deployment is documented in this repository. The production surface here is Docker Compose with Caddy and an observability stack.</p>
              </div>
              <div className="evaluation-path-grid">
                {deploymentPaths.map((path) => (
                  <article className="evaluation-path-card" key={path.label}>
                    <LabelText as="div">{path.label}</LabelText>
                    <h3>{path.title}</h3>
                    <p>{path.copy}</p>
                    <EvidenceLink href={path.evidence} />
                  </article>
                ))}
              </div>
            </section>

            <section className="docs-section tone" id="prerequisites">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">PRODUCTION GATE</LabelText>
                  <h2 className="docs-section-title">Treat state and recovery as part of deployment.</h2>
                </div>
                <p className="docs-section-copy">The production configuration fails closed around required state and identity. A successful local request is not production readiness evidence.</p>
              </div>
              <div className="evaluation-requirements">
                <article><span>01</span><div><h3>Pin a release set</h3><p>Use non-latest gateway and worker images, one release ID, and one worker protocol version.</p></div></article>
                <article><span>02</span><div><h3>Provide shared control state</h3><p>Production gateways require PostgreSQL control state and the same provider-credential encryption key on every replica.</p></div></article>
                <article><span>03</span><div><h3>Choose the ledger topology</h3><p>SQLite is limited to one gateway replica. Active-active gateways require a shared PostgreSQL audit ledger.</p></div></article>
                <article><span>04</span><div><h3>Plan recovery before traffic</h3><p>Keep immutable manifests, database backup or PITR, secret version history, and a tested ingress drain and restore path.</p></div></article>
              </div>
              <div className="evaluation-evidence-row">
                <EvidenceLink href={publicEvidenceLinks.productionCompose} />
                <EvidenceLink href={publicEvidenceLinks.deploymentRecovery} />
                <EvidenceLink href={publicEvidenceLinks.sharedAuditLedger} />
              </div>
            </section>

            <section className="docs-section" id="plan">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">30-MINUTE EVALUATION</LabelText>
                  <h2 className="docs-section-title">Prove one layer at a time.</h2>
                </div>
                <p className="docs-section-copy">The time boxes organize the evaluation; they are not setup-time or performance promises. Stop when a prerequisite is missing rather than bypassing it.</p>
              </div>
              <ol className="evaluation-timeline">
                {evaluationSteps.map((step) => (
                  <li key={step.time}>
                    <span className="evaluation-time">{step.time} min</span>
                    <div><h3>{step.title}</h3><p>{step.copy}</p></div>
                  </li>
                ))}
              </ol>
              <div className="docs-actions">
                <Link className="btn-primary docs-action-link" to="/getting-started">START WITH THE QUICKSTART</Link>
                <Link className="btn-quiet" to="/docs">READ THE API CONTRACT</Link>
              </div>
            </section>

            <section className="docs-section tone" id="comparison">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">FACTUAL COMPARISON</LabelText>
                  <h2 className="docs-section-title">Compare ownership before feature lists.</h2>
                </div>
                <p className="docs-section-copy" id="comparison-note">This table describes Infera from repository evidence. External products and services are deliberately marked for independent verification.</p>
              </div>
              <div className="evaluation-table-region" role="region" aria-labelledby="comparison-caption" tabIndex={0}>
                <table className="evaluation-table" aria-describedby="comparison-note">
                  <caption id="comparison-caption">Evaluation responsibilities across API usage, raw serving engines, hosted inference APIs, and Infera</caption>
                  <thead>
                    <tr>
                      <th scope="col">Decision area</th>
                      <th scope="col">OpenAI API usage</th>
                      <th scope="col">Raw serving engine</th>
                      <th scope="col">Hosted inference API</th>
                      <th scope="col">Infera</th>
                    </tr>
                  </thead>
                  <tbody>
                    {comparisonRows.map((row) => (
                      <tr key={row.criterion}>
                        <th scope="row">{row.criterion}</th>
                        <td>{row.openai}</td>
                        <td>{row.engine}</td>
                        <td>{row.hosted}</td>
                        <td className="evaluation-infera-cell">{row.infera}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="evaluation-scroll-hint">On narrow screens, swipe or use Shift + mouse wheel while the comparison is focused.</p>
              <div className="evaluation-evidence-row">
                <EvidenceLink href={publicEvidenceLinks.compatibility} />
                <EvidenceLink href={publicEvidenceLinks.providerConformance} />
                <EvidenceLink href={publicEvidenceLinks.modularBackend} />
              </div>
            </section>

            <section className="docs-section" id="faq">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">BUYER FAQ</LabelText>
                  <h2 className="docs-section-title">Short answers, inspectable sources.</h2>
                </div>
                <p className="docs-section-copy">Each disclosure uses native browser behavior, so it remains operable by keyboard and assistive technology without custom interaction code.</p>
              </div>
              <div className="evaluation-faq-list">
                {faqs.map((faq) => (
                  <details key={faq.question}>
                    <summary>{faq.question}<span aria-hidden="true">+</span></summary>
                    <div className="evaluation-faq-answer">
                      <p>{faq.answer}</p>
                      <EvidenceLink href={faq.evidence} />
                    </div>
                  </details>
                ))}
              </div>
            </section>

            <section className="docs-section tone evaluation-next-step" aria-labelledby="next-step-title">
              <div>
                <LabelText as="div">NEXT STEP</LabelText>
                <h2 className="docs-section-title" id="next-step-title">Keep the evidence chain intact.</h2>
                <p>Move from evaluation to the quickstart, then the API contract, then production operations. Do not promote a local result into a reliability, cost, or performance claim.</p>
              </div>
              <div className="docs-actions">
                <Link className="btn-primary docs-action-link" to="/getting-started">RUN QUICKSTART</Link>
                <Link className="btn-quiet" to="/trust">INSPECT TRUST RECORD</Link>
              </div>
            </section>
          </div>
        </div>
      </main>
      <PublicFooter />
    </AppShell>
  );
}
