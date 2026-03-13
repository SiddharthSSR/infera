import { Link } from 'react-router-dom';

const endpoints = [
  {
    method: 'GET',
    path: '/v1/models',
    description: 'List available models with OpenAI-compatible core fields.',
  },
  {
    method: 'POST',
    path: '/v1/chat/completions',
    description: 'Create chat completions with optional streaming SSE output.',
  },
];

const compatibility = [
  { label: 'Supported request fields', value: 'model, messages, temperature, top_p, max_tokens, stop, stream, seed, presence_penalty, frequency_penalty' },
  { label: 'Streaming format', value: 'text/event-stream with OpenAI-style chunks and data: [DONE]' },
  { label: 'Known differences', value: 'Infera-specific error types and optional extra metadata on /v1/models' },
];

const curlExample = `curl https://inferai.co.in/v1/chat/completions \\
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
    base_url="https://inferai.co.in/v1",
)

resp = client.chat.completions.create(
    model="meta-llama/Meta-Llama-3.1-8B-Instruct",
    messages=[{"role": "user", "content": "Say hello in one line."}],
)

print(resp.choices[0].message.content)`;

export function PublicApiDocs() {
  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-paper)' }}>
      <div className="app-shell" style={{ maxWidth: 1400 }}>
        <header className="top-nav" style={{ alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}>
          <div>
            <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
            <div className="label-text" style={{ marginTop: '0.5rem' }}>PUBLIC API DOCS</div>
          </div>
          <div className="nav-group" style={{ gap: '1rem' }}>
            <Link className="nav-link" to="/getting-started">GETTING STARTED</Link>
            <Link className="nav-link" to="/">LOGIN</Link>
          </div>
        </header>

        <section className="grid-row" style={{ gridTemplateColumns: '1.2fr 0.8fr' }}>
          <div className="cell" style={{ padding: '3rem 2rem' }}>
            <div className="display-text" style={{ textAlign: 'left', border: 'none', padding: 0, fontSize: '5.5rem' }}>
              OPENAI API
            </div>
            <p style={{ marginTop: '1.25rem', maxWidth: 720, fontSize: '1.05rem', color: 'var(--text-secondary)' }}>
              Use Infera as a drop-in OpenAI-compatible gateway for deployed models. The current public surface focuses on chat completions and model discovery, with streaming support and operator-facing metadata where it is safe to expose.
            </p>
          </div>
          <div className="cell" style={{ padding: '3rem 2rem', background: 'var(--bg-accent)' }}>
            <div className="label-text">AT A GLANCE</div>
            <div style={{ marginTop: '1.5rem', display: 'grid', gap: '1.25rem' }}>
              {compatibility.map((item) => (
                <div key={item.label}>
                  <div className="label-text">{item.label}</div>
                  <div className="value-text" style={{ fontSize: '1rem', marginTop: '0.4rem', lineHeight: 1.5 }}>{item.value}</div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr 1fr' }}>
          <div className="cell">
            <div className="label-text">ENDPOINTS</div>
            <div style={{ marginTop: '1.5rem', display: 'grid', gap: '1rem' }}>
              {endpoints.map((endpoint) => (
                <div key={endpoint.path} style={{ paddingBottom: '1rem', borderBottom: 'var(--grid-line)' }}>
                  <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
                    <span className="label-text">{endpoint.method}</span>
                    <code style={{ fontFamily: 'var(--font-mono)', fontSize: '0.9rem' }}>{endpoint.path}</code>
                  </div>
                  <div style={{ marginTop: '0.55rem', color: 'var(--text-secondary)' }}>{endpoint.description}</div>
                </div>
              ))}
            </div>
          </div>
          <div className="cell">
            <div className="label-text">AUTH</div>
            <div className="value-text" style={{ fontSize: '1rem', marginTop: '1rem', lineHeight: 1.6 }}>
              Send your Infera API key as:
            </div>
            <pre className="code-block">Authorization: Bearer inf_...</pre>
            <div className="value-text" style={{ fontSize: '1rem', marginTop: '1.5rem', lineHeight: 1.6 }}>
              The dashboard uses session auth, but public API clients should use Bearer tokens directly.
            </div>
          </div>
        </section>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr 1fr' }}>
          <div className="cell">
            <div className="label-text">CURL EXAMPLE</div>
            <pre className="code-block" style={{ marginTop: '1rem' }}>{curlExample}</pre>
          </div>
          <div className="cell">
            <div className="label-text">PYTHON SDK EXAMPLE</div>
            <pre className="code-block" style={{ marginTop: '1rem' }}>{pythonExample}</pre>
          </div>
        </section>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="cell" style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap', alignItems: 'center' }}>
            <div>
              <div className="label-text">NEXT</div>
              <div className="value-text" style={{ fontSize: '1.1rem' }}>Use the getting-started flow to make your first request in a few minutes.</div>
            </div>
            <Link className="btn-primary" to="/getting-started" style={{ textDecoration: 'none', display: 'inline-flex', alignItems: 'center' }}>
              OPEN QUICKSTART
            </Link>
          </div>
        </section>
      </div>
    </div>
  );
}
