import { Link } from 'react-router-dom';

const steps = [
  {
    label: '1. Get an API key',
    detail: 'Use an existing Infera key or ask your gateway admin to create one.',
  },
  {
    label: '2. Discover models',
    detail: 'Call GET /v1/models to see the currently available model IDs.',
  },
  {
    label: '3. Send a chat request',
    detail: 'Use POST /v1/chat/completions with a Bearer token and a standard OpenAI-style payload.',
  },
  {
    label: '4. Switch to streaming',
    detail: 'Set stream=true to receive OpenAI-style SSE chunks and a final [DONE] marker.',
  },
];

const firstRequest = `curl https://inferai.co.in/v1/models \\
  -H "Authorization: Bearer inf_..."

curl https://inferai.co.in/v1/chat/completions \\
  -H "Authorization: Bearer inf_..." \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "meta-llama/Meta-Llama-3.1-8B-Instruct",
    "messages": [
      {"role": "user", "content": "Summarize what Infera does in one sentence."}
    ]
  }'`;

export function GettingStarted() {
  return (
    <div style={{ minHeight: '100vh', background: 'linear-gradient(180deg, var(--bg-paper) 0%, #f1ede7 100%)' }}>
      <div className="app-shell" style={{ maxWidth: 1200 }}>
        <header className="top-nav" style={{ flexWrap: 'wrap', gap: '1rem' }}>
          <div>
            <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
            <div className="label-text" style={{ marginTop: '0.5rem' }}>GETTING STARTED</div>
          </div>
          <div className="nav-group" style={{ gap: '1rem' }}>
            <Link className="nav-link" to="/docs">API DOCS</Link>
            <Link className="nav-link" to="/">LOGIN</Link>
          </div>
        </header>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr 1fr' }}>
          <div className="cell" style={{ padding: '3rem 2rem' }}>
            <div className="display-text" style={{ textAlign: 'left', border: 'none', padding: 0, fontSize: '4.8rem' }}>
              FIRST REQUEST
            </div>
            <p style={{ marginTop: '1rem', color: 'var(--text-secondary)', maxWidth: 620, fontSize: '1rem', lineHeight: 1.6 }}>
              This is the shortest path from API key to successful inference. The examples below assume you already have a running Infera deployment and at least one model exposed through the gateway.
            </p>
          </div>
          <div className="cell" style={{ padding: '3rem 2rem', background: 'rgba(0,0,0,0.02)' }}>
            <div className="label-text">FOUR-STEP FLOW</div>
            <div style={{ marginTop: '1.25rem', display: 'grid', gap: '1rem' }}>
              {steps.map((step) => (
                <div key={step.label} style={{ paddingBottom: '1rem', borderBottom: 'var(--grid-line)' }}>
                  <div className="label-text">{step.label}</div>
                  <div className="value-text" style={{ fontSize: '1rem', marginTop: '0.45rem', lineHeight: 1.5 }}>{step.detail}</div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="cell">
            <div className="label-text">COPY / RUN</div>
            <pre className="code-block" style={{ marginTop: '1rem' }}>{firstRequest}</pre>
          </div>
        </section>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr 1fr' }}>
          <div className="cell">
            <div className="label-text">WHAT TO CHECK IF IT FAILS</div>
            <ul style={{ marginTop: '1rem', paddingLeft: '1rem', color: 'var(--text-secondary)', lineHeight: 1.8 }}>
              <li>Key is valid and sent as `Authorization: Bearer ...`</li>
              <li>Model ID matches an entry from `/v1/models`</li>
              <li>At least one healthy worker has the model loaded</li>
              <li>Streaming clients read SSE events until `data: [DONE]`</li>
            </ul>
          </div>
          <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
            <div>
              <div className="label-text">NEXT</div>
              <div className="value-text" style={{ fontSize: '1rem', marginTop: '1rem', lineHeight: 1.6 }}>
                Once the first request works, move to the full API docs for request fields, streaming behavior, and compatibility notes.
              </div>
            </div>
            <Link className="btn-primary" to="/docs" style={{ textDecoration: 'none', width: 'fit-content' }}>
              OPEN API DOCS
            </Link>
          </div>
        </section>
      </div>
    </div>
  );
}
