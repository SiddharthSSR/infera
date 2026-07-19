import { Link } from 'react-router-dom';

const architectureStages = [
  {
    number: '01',
    label: 'Client',
    title: 'OpenAI SDK or HTTP',
    description: 'A workspace key authenticates model discovery and chat requests at the public gateway.',
    detail: 'GET /v1/models · POST /v1/chat/completions',
  },
  {
    number: '02',
    label: 'Gateway',
    title: 'Authenticate and route',
    description: 'The gateway resolves the workspace, applies key-level limits, and selects a healthy worker that serves the requested model.',
    detail: 'workspace · model · healthy worker',
  },
  {
    number: '03',
    label: 'Worker',
    title: 'Run the model',
    description: 'The worker passes the request to its configured engine and returns either one response or OpenAI-style SSE chunks.',
    detail: 'engine request · response serialization',
  },
  {
    number: '04',
    label: 'Operator record',
    title: 'Keep the outcome inspectable',
    description: 'The audit path records workspace, key prefix, model, worker, stream mode, status, error, latency, and token counts—not prompt text or key material.',
    detail: 'usage ledger · route metadata · deployment history',
  },
] as const;

const sourceLinks = [
  {
    label: 'Read the compatibility contract',
    href: '/docs',
    internal: true,
  },
  {
    label: 'Inspect the gateway request path',
    href: 'https://github.com/SiddharthSSR/infera/blob/17d0e16233d6db13691e7f3c288d3d39d78eec37/go/internal/gateway/inference_service.go',
    internal: false,
  },
  {
    label: 'Inspect the worker HTTP runtime',
    href: 'https://github.com/SiddharthSSR/infera/blob/17d0e16233d6db13691e7f3c288d3d39d78eec37/python/src/infera_worker/http_server.py',
    internal: false,
  },
] as const;

export function TechnicalProof() {
  return (
    <section className="landing-section landing-technical-proof" id="architecture" aria-labelledby="architecture-heading">
      <div className="landing-section-heading">
        <div>
          <span className="landing-meta">Request architecture</span>
          <h2 id="architecture-heading">One request. Four inspectable boundaries.</h2>
        </div>
        <p>
          Infera sits between client code and model runtimes. It keeps the public contract small while giving operators a workspace-scoped path to deploy, route, verify, and account for inference.
        </p>
      </div>

      <ol className="landing-architecture-flow" aria-label="Inference request data flow">
        {architectureStages.map((stage) => (
          <li key={stage.number}>
            <div className="landing-architecture-index" aria-hidden="true">{stage.number}</div>
            <div className="landing-architecture-copy">
              <span className="landing-meta">{stage.label}</span>
              <h3>{stage.title}</h3>
              <p>{stage.description}</p>
              <code>{stage.detail}</code>
            </div>
          </li>
        ))}
      </ol>

      <div className="landing-source-row" aria-label="Architecture sources">
        <span className="landing-meta">Verify in source</span>
        <div>
          {sourceLinks.map((source) => (
            source.internal ? (
              <Link key={source.href} to={source.href}>{source.label}</Link>
            ) : (
              <a key={source.href} href={source.href} target="_blank" rel="noreferrer">{source.label}</a>
            )
          ))}
        </div>
      </div>
    </section>
  );
}
