import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { AppShell, PublicFooter, PublicNav } from '../components/shared';
import { ProductWalkthrough } from '../components/public/ProductWalkthrough';
import {
  designPartnerRequestEndpoint,
  getPublicAcquisitionTarget,
} from '../lib/designPartnerRequest';
import { publicAnalytics } from '../lib/publicAnalytics';

const registryModels = [
  {
    mark: 'MI',
    tag: 'GENERAL',
    name: 'Mistral 7B Instruct v0.3',
    source: 'mistralai/Mistral-7B-Instruct-v0.3',
    description: 'General-purpose chat and instruction following.',
  },
  {
    mark: 'L3',
    tag: '8B',
    name: 'Llama 3.1 8B Instruct',
    source: 'meta-llama/Meta-Llama-3.1-8B-Instruct',
    description: 'Instruction-tuned model for chat and coding workflows.',
  },
  {
    mark: 'P3',
    tag: 'COMPACT',
    name: 'Phi-3 Mini 4K Instruct',
    source: 'microsoft/Phi-3-mini-4k-instruct',
    description: 'Compact instruction model with a smaller runtime footprint.',
  },
  {
    mark: 'Q2',
    tag: '7B',
    name: 'Qwen2.5 7B Instruct',
    source: 'Qwen/Qwen2.5-7B-Instruct',
    description: 'Multilingual chat and instruction model.',
  },
  {
    mark: 'Q3',
    tag: 'REASONING',
    name: 'Qwen3 4B Thinking 2507',
    source: 'Qwen/Qwen3-4B-Thinking-2507',
    description: 'Compact reasoning-oriented model from the seeded registry.',
  },
  {
    mark: 'CL',
    tag: '13B',
    name: 'CodeLlama 13B Instruct',
    source: 'codellama/CodeLlama-13b-Instruct-hf',
    description: 'Instruction-tuned model for code-focused workloads.',
  },
] as const;

const baseUrl = window.location.origin;
const pythonExample = `from openai import OpenAI

client = OpenAI(
  api_key="YOUR_INFERA_KEY",
  base_url="${baseUrl}/v1",
)`;

export interface PublicLandingProps {
  intakeEndpoint?: string;
}

export function PublicLanding({ intakeEndpoint = designPartnerRequestEndpoint }: PublicLandingProps) {
  const [copyStatus, setCopyStatus] = useState('');
  const copyResetTimer = useRef<number>();
  const acquisition = getPublicAcquisitionTarget(intakeEndpoint);
  const acquisitionLabel = acquisition.path === '/request-access'
    ? 'Request design-partner access'
    : 'Evaluate deployment fit';

  useEffect(() => () => window.clearTimeout(copyResetTimer.current), []);
  useEffect(() => {
    publicAnalytics.track('public_landing_view', { surface: 'migration_landing' });
  }, []);

  const trackQuickstart = () => {
    publicAnalytics.track('public_primary_cta_clicked', { action: 'start_building', placement: 'hero' });
    publicAnalytics.track('public_resource_opened', { resource: 'quickstart', source: 'landing' });
  };

  const trackAcquisition = (placement: 'hero' | 'closing') => {
    publicAnalytics.track('public_primary_cta_clicked', { action: acquisition.action, placement });
  };

  const copyExample = async () => {
    window.clearTimeout(copyResetTimer.current);

    try {
      await navigator.clipboard.writeText(pythonExample);
      setCopyStatus('Copied to clipboard.');
    } catch {
      setCopyStatus('Copy failed. Select the code to copy it manually.');
    }

    copyResetTimer.current = window.setTimeout(() => setCopyStatus(''), 3000);
  };

  return (
    <AppShell variant="public" className="public-landing-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="OPEN INFERENCE CONTROL PLANE" intakeEndpoint={intakeEndpoint} />

      <main id="main-content">
        <section className="landing-hero" aria-labelledby="landing-title">
          <div className="landing-hero-copy">
            <div className="landing-eyebrow">Open model gateway + control plane</div>
            <h1 id="landing-title">Run open models. Keep your OpenAI client.</h1>
            <p className="landing-lede">
              One compatible endpoint for model discovery, chat, and streaming—plus the operator controls to keep it serving.
            </p>
            <div className="landing-actions">
              <Link className="landing-button landing-button-primary" to={acquisition.path} onClick={() => trackAcquisition('hero')}>{acquisitionLabel}</Link>
              <Link className="landing-button landing-button-secondary" to="/getting-started" onClick={trackQuickstart}>Run the quickstart</Link>
              <a
                className="landing-button landing-button-secondary"
                href="#models"
                onClick={() => publicAnalytics.track('public_product_explored', { product: 'model_catalog', source: 'landing' })}
              >Explore registry models</a>
            </div>
          </div>

          <aside className="landing-proof" aria-label="OpenAI client migration example">
            <div className="landing-proof-header">
              <span className="landing-meta">Client change</span>
              <span className="landing-meta landing-status">OpenAI SDK flow</span>
            </div>
            <div className="landing-proof-body">
              <h2>Two lines change. Your client flow stays.</h2>
              <div className="landing-code-shell">
                <button
                  type="button"
                  className="landing-copy-button"
                  data-copy-state={copyStatus ? (copyStatus.startsWith('Copied') ? 'success' : 'error') : 'idle'}
                  aria-describedby="landing-copy-status"
                  onClick={() => void copyExample()}
                >
                  {copyStatus.startsWith('Copied') ? 'Copied' : copyStatus ? 'Try again' : 'Copy'}
                </button>
                <pre tabIndex={0}><code>{pythonExample}</code></pre>
              </div>
              <div id="landing-copy-status" className="landing-copy-status" role="status" aria-live="polite">{copyStatus}</div>
            </div>
            <dl className="landing-proof-list">
              <div><dt>Discover</dt><dd>Read live model IDs from <code>/v1/models</code>.</dd></div>
              <div><dt>Run</dt><dd>Send unary or streaming chat completions.</dd></div>
            </dl>
          </aside>
        </section>

        <section className="landing-signal-strip" aria-label="Current product surface">
          <div><span>01 / Discover</span><strong>Live model IDs</strong></div>
          <div><span>02 / Connect</span><strong>OpenAI-style client</strong></div>
          <div><span>03 / Serve</span><strong>Unary + streaming</strong></div>
          <div><span>04 / Inspect</span><strong>Workers, routes, usage</strong></div>
        </section>

        <section className="landing-section landing-model-library" id="models" aria-labelledby="models-heading">
          <div className="landing-section-heading">
            <div><span className="landing-meta">Seeded registry</span><h2 id="models-heading">Put open models behind one endpoint.</h2></div>
            <div className="landing-section-aside">
              <p>Start from the built-in registry or register another model source. Live serving still requires a healthy worker.</p>
              <Link className="landing-inline-link" to="/getting-started#copy-run">See model discovery →</Link>
            </div>
          </div>
          <div className="landing-model-grid">
            {registryModels.map((model) => (
              <article key={model.source}>
                <div className="landing-model-meta">
                  <span className="landing-model-mark" aria-hidden="true">{model.mark}</span>
                  <span className="landing-model-tag">{model.tag}</span>
                </div>
                <h3>{model.name}</h3>
                <p>{model.description}</p>
                <code>{model.source}</code>
              </article>
            ))}
          </div>
          <p className="landing-model-note"><strong>A registry entry does not mean serving.</strong> Use <code>GET /v1/models</code> and worker health as the source of truth before sending traffic.</p>
        </section>

        <ProductWalkthrough />

        <section className="landing-section landing-section-tone landing-boundary-section" id="proof" aria-labelledby="proof-heading">
          <div className="landing-section-heading">
            <div><span className="landing-meta">Public API boundary</span><h2 id="proof-heading">Small surface. Clear limits.</h2></div>
            <p>Use the compatibility that exists today, with no implied support for endpoints that are not shipped.</p>
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
                <div><dt>Surface</dt><dd>No legacy completions or embeddings endpoint.</dd></div>
              </dl>
            </div>
          </div>
          <div className="landing-proof-links">
            <Link to="/docs" onClick={() => publicAnalytics.track('public_resource_opened', { resource: 'api_docs', source: 'landing' })}>Read the API contract →</Link>
            <Link to="/trust">Inspect the trust record →</Link>
          </div>
        </section>

        <section className="landing-final-cta" aria-labelledby="final-cta-heading">
          <div><span className="landing-meta">No paid subscription required</span><h2 id="final-cta-heading">Bring us the inference problem you need to evaluate.</h2></div>
          <Link className="landing-button" to={acquisition.path} onClick={() => trackAcquisition('closing')}>{acquisitionLabel}</Link>
        </section>
      </main>

      <PublicFooter intakeEndpoint={intakeEndpoint} />
    </AppShell>
  );
}
