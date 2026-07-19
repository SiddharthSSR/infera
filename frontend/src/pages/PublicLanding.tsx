import { useState } from 'react';
import { Link } from 'react-router-dom';
import { AppShell, PublicNav } from '../components/shared';

const migrationSteps = [
  {
    number: '01',
    title: 'Confirm auth',
    description: 'Use a workspace-scoped Bearer token. Browser sign-in and machine access stay separate.',
  },
  {
    number: '02',
    title: 'List live models',
    description: 'Call /v1/models and use the returned model ID as the source of truth for the request.',
  },
  {
    number: '03',
    title: 'Send one chat',
    description: 'Start with a small non-streaming request so auth, routing, and availability are easy to isolate.',
  },
  {
    number: '04',
    title: 'Promote to stream',
    description: 'Turn on SSE after the unary path works, then read through the final [DONE] marker.',
  },
];

const productSurfaces = [
  { label: 'Models', title: 'Know what can serve', description: 'Inspect the available model surface before clients depend on it.' },
  { label: 'Nodes', title: 'See runtime capacity', description: 'Keep instance readiness near the models those nodes support.' },
  { label: 'Playground', title: 'Test the real route', description: 'Exercise a workspace request before changing application traffic.' },
  { label: 'Logs', title: 'Trace request behavior', description: 'Separate authentication, routing, runtime, and model issues.' },
  { label: 'API keys', title: 'Scope machine access', description: 'Keep service credentials distinct from human dashboard sessions.' },
  { label: 'Workspace', title: 'Operate as a team', description: 'Centralize access and settings around the serving workspace.' },
];

const baseUrl = window.location.origin;
const pythonExample = `from openai import OpenAI

client = OpenAI(
  api_key="YOUR_INFERA_KEY",
  base_url="${baseUrl}/v1",
)`;

export function PublicLanding() {
  const [copyStatus, setCopyStatus] = useState('');

  const copyExample = async () => {
    try {
      await navigator.clipboard.writeText(pythonExample);
      setCopyStatus('Copied to clipboard.');
    } catch {
      setCopyStatus('Copy failed. Select the code to copy it manually.');
    }
  };

  return (
    <AppShell variant="public" className="public-landing-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="OPEN INFERENCE CONTROL PLANE" />

      <main id="main-content">
        <section className="landing-hero" aria-labelledby="landing-title">
          <div className="landing-hero-copy">
            <div className="landing-eyebrow">OpenAI-compatible inference control plane</div>
            <h1 id="landing-title">Run open models behind the client you already ship.</h1>
            <p className="landing-lede">
              Infera gives infrastructure teams an OpenAI-style gateway for model discovery, chat completions,
              and streaming, with the operator surfaces needed to keep that path working.
            </p>
            <div className="landing-actions">
              <Link className="landing-button landing-button-primary" to="/getting-started">Run the migration quickstart</Link>
              <a className="landing-button landing-button-secondary" href="#product">See the control plane</a>
            </div>
          </div>

          <aside className="landing-proof" aria-label="OpenAI client migration example">
            <div className="landing-proof-header">
              <span className="landing-meta">Client change</span>
              <span className="landing-meta landing-status">OpenAI SDK flow</span>
            </div>
            <div className="landing-proof-body">
              <h2>Change the endpoint. Keep the client workflow.</h2>
              <div className="landing-code-shell">
                <button type="button" className="landing-copy-button" onClick={() => void copyExample()}>Copy</button>
                <pre tabIndex={0}><code>{pythonExample}</code></pre>
              </div>
              <div className="landing-copy-status" role="status" aria-live="polite">{copyStatus}</div>
            </div>
            <dl className="landing-proof-list">
              <div><dt>Discover</dt><dd>Read live model IDs from <code>/v1/models</code>.</dd></div>
              <div><dt>Request</dt><dd>Send OpenAI-style chat completion payloads.</dd></div>
              <div><dt>Stream</dt><dd>Receive SSE chunks through the same client flow.</dd></div>
            </dl>
          </aside>
        </section>

        <section className="landing-signal-strip" aria-label="Current product surface">
          <div><span>01 / Gateway</span><strong>Workspace-scoped auth</strong></div>
          <div><span>02 / API</span><strong>Model discovery</strong></div>
          <div><span>03 / Requests</span><strong>Unary + streaming</strong></div>
          <div><span>04 / Operate</span><strong>Models, nodes, logs</strong></div>
        </section>

        <section className="landing-section" id="migration" aria-labelledby="migration-heading">
          <div className="landing-section-heading">
            <div><span className="landing-meta">Migration runbook</span><h2 id="migration-heading">First response before first surprise.</h2></div>
            <p>A deliberately narrow path from key to working request. Each step removes one source of uncertainty before the next.</p>
          </div>
          <ol className="landing-step-grid">
            {migrationSteps.map((step) => (
              <li key={step.number}>
                <span className="landing-meta">Step {step.number}</span>
                <h3>{step.title}</h3>
                <p>{step.description}</p>
              </li>
            ))}
          </ol>
          <div className="landing-actions">
            <Link className="landing-button landing-button-primary" to="/getting-started">Open the full quickstart</Link>
            <Link className="landing-button landing-button-secondary" to="/docs">Read API boundaries</Link>
          </div>
        </section>

        <section className="landing-section landing-section-tone" id="product" aria-labelledby="product-heading">
          <div className="landing-section-heading">
            <div><span className="landing-meta">Product</span><h2 id="product-heading">The gateway is the entry point. The control plane is the product.</h2></div>
            <p>The same workspace that serves the compatible endpoint gives operators a place to inspect the serving path and manage access.</p>
          </div>
          <div className="landing-surface-grid">
            {productSurfaces.map((surface) => (
              <article key={surface.label}>
                <span className="landing-meta">{surface.label}</span>
                <h3>{surface.title}</h3>
                <p>{surface.description}</p>
              </article>
            ))}
          </div>
        </section>

        <section className="landing-section" id="proof" aria-labelledby="proof-heading">
          <div className="landing-section-heading">
            <div><span className="landing-meta">Public API boundary</span><h2 id="proof-heading">A small contract, stated plainly.</h2></div>
            <p>Evaluate the interface and its known differences directly, without unsupported performance or adoption claims.</p>
          </div>
          <div className="landing-boundary-grid">
            <div>
              <h3>What is available</h3>
              <dl>
                <div><dt>Discovery</dt><dd><code>GET /v1/models</code> for live model IDs.</dd></div>
                <div><dt>Chat</dt><dd><code>POST /v1/chat/completions</code> with OpenAI-style payloads.</dd></div>
                <div><dt>Streaming</dt><dd>SSE chunks with a final <code>data: [DONE]</code> marker.</dd></div>
              </dl>
            </div>
            <div>
              <h3>Where it differs</h3>
              <dl>
                <div><dt>Errors</dt><dd>Error types are Infera-specific.</dd></div>
                <div><dt>Metadata</dt><dd>Model discovery may expose extra safe operator metadata.</dd></div>
                <div><dt>Auth</dt><dd>Public API keys and browser sessions are separate concerns.</dd></div>
              </dl>
            </div>
          </div>
        </section>

        <section className="landing-final-cta" aria-labelledby="final-cta-heading">
          <h2 id="final-cta-heading">Start with one compatible request.</h2>
          <Link className="landing-button" to="/getting-started">Run the quickstart</Link>
        </section>
      </main>

      <footer className="landing-footer">
        <span>INFERA.AI</span>
        <span>Open-source inference gateway</span>
      </footer>
    </AppShell>
  );
}
