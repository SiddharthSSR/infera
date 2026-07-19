import { Link } from 'react-router-dom';
import { CodeExample } from '../components/CodeExample';
import { LabelText, Badge, AppShell, PublicNav } from '../components/shared';

const BASE_URL = typeof window !== 'undefined' ? window.location.origin : 'https://inferai.co.in';

const summaryCards = [
  {
    label: 'Base URL',
    value: `${BASE_URL}/v1`,
    tone: 'code',
  },
  {
    label: 'Primary surface',
    value: 'OpenAI-style model discovery and chat completions with SSE streaming.',
  },
  {
    label: 'Best fit',
    value: 'Teams that want a hosted inference gateway but keep the OpenAI client workflow.',
  },
];

const quickstartSteps = [
  {
    number: '01',
    title: 'Bring a key',
    copy: 'Use a workspace-scoped API key. Prefer a service-account key for automation and keep human keys for dashboard sign-in.',
  },
  {
    number: '02',
    title: 'List models',
    copy: 'Call GET /v1/models first. Treat the returned id as the source of truth for requests.',
  },
  {
    number: '03',
    title: 'Send chat',
    copy: 'Use POST /v1/chat/completions with a standard OpenAI message payload.',
  },
  {
    number: '04',
    title: 'Turn on stream',
    copy: 'Set stream=true when you want SSE chunks followed by data: [DONE].',
  },
];

const endpoints = [
  {
    method: 'GET',
    path: '/v1/models',
    title: 'List models',
    description: 'Returns the currently available model ids and OpenAI-compatible core metadata.',
    request: 'Authorization: Bearer inf_...',
    response: 'object=list, data=[{ id, object, created, owned_by, ... }]',
  },
  {
    method: 'POST',
    path: '/v1/chat/completions',
    title: 'Create chat completion',
    description: 'Accepts OpenAI-style chat payloads and returns either a single response body or SSE chunks.',
    request: 'model, messages, temperature, top_p, max_tokens, stop, stream, seed, presence_penalty, frequency_penalty',
    response: 'OpenAI-style choices/message usage body or text/event-stream chunks',
  },
];

const compatibilityCards = [
  {
    label: 'Supported request fields',
    value: 'model, messages, temperature, top_p, max_tokens, stop, stream, seed, presence_penalty, frequency_penalty',
  },
  {
    label: 'Streaming contract',
    value: 'SSE with OpenAI-style chunk objects and a final data: [DONE] marker.',
  },
  {
    label: 'Known differences',
    value: 'Error types are Infera-specific and /v1/models may expose extra safe operator metadata.',
  },
  {
    label: 'Auth model',
    value: 'Public API access uses workspace-scoped Bearer tokens. Use service-account keys for automation; browser sessions are separate human auth.',
  },
];

const curlExample = `curl ${BASE_URL}/v1/chat/completions \\
  -H "Authorization: Bearer inf_..." \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "meta-llama/Meta-Llama-3.1-8B-Instruct",
    "messages": [
      {"role": "user", "content": "Say hello in one line."}
    ]
  }'`;

const pythonExample = `from openai import OpenAI

client = OpenAI(
    api_key="YOUR_INFERA_KEY",
    base_url="${BASE_URL}/v1",
)

resp = client.chat.completions.create(
    model="meta-llama/Meta-Llama-3.1-8B-Instruct",
    messages=[{"role": "user", "content": "Say hello in one line."}],
)

print(resp.choices[0].message.content)`;

const typescriptExample = `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.INFERA_API_KEY,
  baseURL: "${BASE_URL}/v1",
});

const resp = await client.chat.completions.create({
  model: "meta-llama/Meta-Llama-3.1-8B-Instruct",
  messages: [{ role: "user", content: "Say hello in one line." }],
  stream: true,
});

for await (const chunk of resp) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? "");
}`;

export function PublicApiDocs() {
  return (
    <AppShell variant="public">
        <a className="public-skip-link" href="#main-content">Skip to main content</a>
        <PublicNav title="PUBLIC API DOCS" />

        <main id="main-content">
        <section className="docs-hero">
          <div className="docs-kicker">OpenAI-compatible gateway</div>
          <div className="docs-hero-grid">
            <div>
              <h1 className="docs-title">Build against Infera without rewriting your client.</h1>
              <p className="docs-subtitle">
                Infera gives you a production inference gateway with an OpenAI-style interface. The public surface stays focused: discover models, send chat requests, and stream tokens with the same client flow your team already understands.
              </p>
              <div className="docs-hero-strip">
                <span className="docs-pill">OpenAI client flow</span>
                <span className="docs-pill">Streaming SSE</span>
                <span className="docs-pill">Workspace keys</span>
              </div>
              <div className="docs-actions">
                <Link className="btn-primary" to="/getting-started" style={{ textDecoration: 'none' }}>
                  START QUICKSTART
                </Link>
                <a className="btn-quiet" href="#examples">SEE EXAMPLES</a>
              </div>
            </div>
            <div className="docs-summary">
              {summaryCards.map((card) => (
                <div key={card.label} className="docs-summary-card">
                  <LabelText as="div">{card.label}</LabelText>
                  <div className={`docs-summary-value ${card.tone === 'code' ? 'docs-summary-code' : ''}`}>{card.value}</div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <div className="docs-layout">
          <aside className="docs-sidebar">
            <LabelText as="div">ON THIS PAGE</LabelText>
            <nav className="docs-sidebar-nav" aria-label="On this page">
              <a className="docs-sidebar-link" href="#quickstart">Quickstart flow</a>
              <a className="docs-sidebar-link" href="#endpoints">Endpoints</a>
              <a className="docs-sidebar-link" href="#authentication">Authentication</a>
              <a className="docs-sidebar-link" href="#compatibility">Compatibility</a>
              <a className="docs-sidebar-link" href="#examples">Examples</a>
            </nav>
            <div className="docs-sidebar-card">
              <LabelText as="div">START HERE</LabelText>
              <div style={{ marginTop: '0.7rem', color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.65 }}>
                If this is your first call, do not start here. Use the quickstart first, then come back for the surface details.
              </div>
            </div>
          </aside>

          <div className="docs-main">
            <section className="docs-section" id="quickstart">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">QUICKSTART FLOW</LabelText>
                  <h2 className="docs-section-title">Four moves to the first working request.</h2>
                </div>
                <div className="docs-section-copy">
                  This is the path we optimize for: get a key, read the live model list, send chat, then switch to streaming once the first unary request works.
                </div>
              </div>
              <div className="docs-step-grid">
                {quickstartSteps.map((step) => (
                  <div key={step.number} className="docs-step-card">
                    <div className="docs-step-number">Step {step.number}</div>
                    <div className="docs-step-title">{step.title}</div>
                    <div className="docs-step-copy">{step.copy}</div>
                  </div>
                ))}
              </div>
            </section>

            <section className="docs-section tone" id="endpoints">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">ENDPOINT SURFACE</LabelText>
                  <h2 className="docs-section-title">Small surface area, explicit behavior.</h2>
                </div>
                <div className="docs-section-copy">
                  The current public API is intentionally narrow. It is optimized for reliability and compatibility instead of pretending to support the whole OpenAI surface prematurely.
                </div>
              </div>
              {endpoints.map((endpoint) => (
                <div key={endpoint.path} className="docs-endpoint-card">
                  <div className="docs-endpoint-line">
                    <span className={`docs-method-pill ${endpoint.method.toLowerCase()}`}>{endpoint.method}</span>
                    <span className="docs-path">{endpoint.path}</span>
                  </div>
                  <h3 style={{ marginTop: '0.85rem', fontSize: '1.35rem' }}>{endpoint.title}</h3>
                  <p style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.7 }}>
                    {endpoint.description}
                  </p>
                  <div className="docs-meta-list">
                    <div className="docs-meta-row">
                      <LabelText>REQUEST</LabelText>
                      <span>{endpoint.request}</span>
                    </div>
                    <div className="docs-meta-row">
                      <LabelText>RESPONSE</LabelText>
                      <span>{endpoint.response}</span>
                    </div>
                  </div>
                </div>
              ))}
            </section>

            <section className="docs-section" id="authentication">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">AUTHENTICATION</LabelText>
                  <h2 className="docs-section-title">Bearer token in, request out.</h2>
                </div>
                <div className="docs-section-copy">
                  The public API does not use browser sessions. Treat it like any other machine-to-machine API and send your workspace key with each request. For production clients, use a service-account key instead of a human dashboard key.
                </div>
              </div>
              <div className="docs-card-grid">
                <div className="docs-card">
                  <LabelText as="div">HEADER</LabelText>
                  <CodeExample code={'Authorization: Bearer inf_...'} language="text" style={{ marginTop: '1rem' }} />
                </div>
                <div className="docs-card">
                  <LabelText as="div">IMPORTANT</LabelText>
                  <div className="docs-list">
                    <div>Use the model id returned by <span className="docs-inline-code">/v1/models</span>.</div>
                    <div>Dashboard login and public API auth are separate concerns.</div>
                    <div>Service-account keys are the safer default for integrations and CI.</div>
                  </div>
                </div>
              </div>
            </section>

            <section className="docs-section tone" id="compatibility">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">COMPATIBILITY NOTES</LabelText>
                  <h2 className="docs-section-title">Compatible where it matters, explicit where it differs.</h2>
                </div>
                <div className="docs-section-copy">
                  The contract is designed to work with existing OpenAI clients while still leaving room for Infera-specific operator metadata where it is useful and safe.
                </div>
              </div>
              <div className="docs-card-grid">
                {compatibilityCards.map((card) => (
                  <div key={card.label} className="docs-card">
                    <LabelText as="div">{card.label}</LabelText>
                    <div style={{ marginTop: '0.7rem', color: 'var(--text-secondary)', lineHeight: 1.65 }}>{card.value}</div>
                  </div>
                ))}
              </div>
            </section>

            <section className="docs-section" id="examples">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">EXAMPLES</LabelText>
                  <h2 className="docs-section-title">Copy, run, adapt.</h2>
                </div>
                <div className="docs-section-copy">
                  Start with the curl path. Once it works, switch the same base URL into your OpenAI client and keep the rest of the app code nearly unchanged.
                </div>
              </div>
              <div className="docs-code-grid">
                <div className="docs-code-panel">
                  <div className="docs-code-toolbar">
                    <LabelText as="div">CURL</LabelText>
                    <Badge>UNARY</Badge>
                  </div>
                  <CodeExample code={curlExample} language="shell" />
                </div>
                <div className="docs-code-panel">
                  <div className="docs-code-toolbar">
                    <LabelText as="div">PYTHON SDK</LabelText>
                    <Badge>OPENAI CLIENT</Badge>
                  </div>
                  <CodeExample code={pythonExample} language="python" />
                </div>
                <div className="docs-code-panel" style={{ gridColumn: '1 / -1' }}>
                  <div className="docs-code-toolbar">
                    <LabelText as="div">TYPESCRIPT SDK</LabelText>
                    <Badge>STREAMING</Badge>
                  </div>
                  <CodeExample code={typescriptExample} language="typescript" />
                </div>
              </div>
              <div className="docs-callout" style={{ marginTop: '1rem' }}>
                If you are testing streaming, make sure your client reads the full SSE stream and waits for the final <span className="docs-inline-code">data: [DONE]</span> marker before treating the response as complete.
              </div>
            </section>
          </div>
        </div>
        </main>
    </AppShell>
  );
}
