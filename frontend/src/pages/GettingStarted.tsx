import { Link } from 'react-router-dom';
import { CodeExample } from '../components/CodeExample';
import { LabelText, Badge, AppShell, PublicNav } from '../components/shared';

const BASE_URL = typeof window !== 'undefined' ? window.location.origin : 'https://inferai.co.in';

const prepCards = [
  {
    label: 'You need',
    value: 'A workspace-scoped Infera API key with access to at least one model. Use a service-account key for automation when possible.',
  },
  {
    label: 'Best workflow',
    value: 'Works with any OpenAI-compatible SDK. Set base_url to your gateway and use your inf_... key.',
  },
  {
    label: 'Success signal',
    value: 'One non-streaming response first. Add stream=true only after that passes.',
  },
];

const credentialPaths = [
  {
    label: 'Machine request',
    value: 'Use a workspace API key in the Authorization header. Prefer a service-account key for scripts, servers, and production automation.',
  },
  {
    label: 'Human dashboard session',
    value: 'Sign in with your human key to create a browser session. The session is for operating the workspace; it is not the credential your deployed client sends to /v1.',
  },
];

const steps = [
  {
    number: '01',
    title: 'Confirm auth',
    copy: 'Use the key as Authorization: Bearer inf_... and treat it like a service credential, not a browser login. Prefer a service-account key for scripts and production clients.',
  },
  {
    number: '02',
    title: 'Inspect live models',
    copy: 'The model id in your request should come from the current /v1/models response, not from memory or docs screenshots.',
  },
  {
    number: '03',
    title: 'Send a chat request',
    copy: 'Keep the first prompt small and deterministic so auth, routing, and model availability are easy to validate.',
  },
  {
    number: '04',
    title: 'Promote to streaming',
    copy: 'Switch to stream=true after the unary path works. Streaming issues are easier to isolate that way.',
  },
];

const modelsRequest = `curl ${BASE_URL}/v1/models \\
  -H "Authorization: Bearer inf_..."`;

const chatRequest = `curl ${BASE_URL}/v1/chat/completions \\
  -H "Authorization: Bearer inf_..." \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "Qwen/Qwen2.5-7B-Instruct",
    "messages": [
      {"role": "user", "content": "Summarize what Infera does in one sentence."}
    ]
  }'`;

const streamRequest = `curl ${BASE_URL}/v1/chat/completions \\
  -H "Authorization: Bearer inf_..." \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "Qwen/Qwen2.5-7B-Instruct",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Say hello in one short line."}
    ]
  }'`;

const pythonSdkExample = `from openai import OpenAI

client = OpenAI(
    base_url="${BASE_URL}/v1",
    api_key="inf_..."
)

response = client.chat.completions.create(
    model="Qwen/Qwen2.5-7B-Instruct",
    messages=[
        {"role": "user", "content": "Summarize what Infera does in one sentence."}
    ]
)
print(response.choices[0].message.content)`;

const jsSdkExample = `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${BASE_URL}/v1",
  apiKey: "inf_...",
});

const response = await client.chat.completions.create({
  model: "Qwen/Qwen2.5-7B-Instruct",
  messages: [
    { role: "user", content: "Summarize what Infera does in one sentence." }
  ],
});
console.log(response.choices[0].message.content);`;

const failureChecks = [
  'The key is valid and sent as Authorization: Bearer ...',
  'The model id exactly matches a value from /v1/models',
  'At least one healthy worker has that model available',
  'Your client keeps reading the stream until data: [DONE]',
];

export function GettingStarted() {
  return (
    <AppShell variant="public">
        <a className="public-skip-link" href="#main-content">Skip to main content</a>
        <PublicNav title="GETTING STARTED" />

        <main id="main-content">
        <section className="docs-hero">
          <div className="docs-kicker">First successful request</div>
          <div className="docs-hero-grid">
            <div>
              <h1 className="docs-title">From API key to first model response.</h1>
              <p className="docs-subtitle">
                This page is the shortest production path into Infera. Keep the first call boring: validate auth, read the live model list, ship one unary request, then move to streaming once the basics are proven.
              </p>
              <div className="docs-hero-strip">
                <span className="docs-pill">Model list first</span>
                <span className="docs-pill">Unary before stream</span>
                <span className="docs-pill">Production-safe flow</span>
              </div>
              <div className="docs-actions">
                <a className="btn-primary" href="#runbook" style={{ textDecoration: 'none' }}>RUN THE FLOW</a>
                <Link className="btn-quiet" to="/docs">OPEN FULL API DOCS</Link>
                <Link className="btn-quiet" to="/sign-in">SIGN IN TO DASHBOARD</Link>
              </div>
            </div>
            <div className="docs-summary">
              {prepCards.map((card) => (
                <div key={card.label} className="docs-summary-card">
                  <LabelText as="div">{card.label}</LabelText>
                  <div className="docs-summary-value">{card.value}</div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <div className="docs-layout">
          <aside className="docs-sidebar">
            <LabelText as="div">QUICK NAV</LabelText>
            <nav className="docs-sidebar-nav" aria-label="On this page">
              <a className="docs-sidebar-link" href="#runbook">Runbook</a>
              <a className="docs-sidebar-link" href="#credentials">Choose a credential</a>
              <a className="docs-sidebar-link" href="#copy-run">Copy and run</a>
              <a className="docs-sidebar-link" href="#sdk-examples">SDK examples</a>
              <a className="docs-sidebar-link" href="#streaming">Streaming</a>
              <a className="docs-sidebar-link" href="#failures">Failure checks</a>
            </nav>
            <div className="docs-sidebar-card">
              <LabelText as="div">RULE</LabelText>
              <div style={{ marginTop: '0.7rem', color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.65 }}>
                Always promote complexity in order: auth, live model lookup, unary request, then streaming.
              </div>
            </div>
          </aside>

          <div className="docs-main">
            <section className="docs-section tone" id="credentials">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">BEFORE YOU START</LabelText>
                  <h2 className="docs-section-title">Choose the credential for the job.</h2>
                </div>
                <div className="docs-section-copy">
                  An API key can establish access, but a machine request and a human browser session are different trust contexts. Do not put a human dashboard key into unattended production code.
                </div>
              </div>
              <div className="docs-card-grid">
                {credentialPaths.map((path) => (
                  <div key={path.label} className="docs-card">
                    <LabelText as="div">{path.label}</LabelText>
                    <div style={{ marginTop: '0.7rem', color: 'var(--text-secondary)', lineHeight: 1.7 }}>
                      {path.value}
                    </div>
                  </div>
                ))}
              </div>
              <div className="docs-card docs-recovery-card">
                <LabelText as="div">NO KEY OR WRONG WORKSPACE?</LabelText>
                <div className="docs-recovery-copy">
                  Ask a workspace admin for an invitation if you need human access. If you already belong to the workspace, sign in and create a service-account key from API Keys for automation. After sign-in, your first setup action is to connect provider access in Workspace.
                </div>
                <div className="docs-actions docs-recovery-actions">
                  <Link className="btn-primary docs-action-link" to="/sign-in">SIGN IN</Link>
                  <Link className="btn-quiet" to="/accept-invite">ACCEPT AN INVITATION</Link>
                </div>
              </div>
            </section>

            <section className="docs-section" id="runbook">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">RUNBOOK</LabelText>
                  <h2 className="docs-section-title">Use this order. It reduces debugging time.</h2>
                </div>
                <div className="docs-section-copy">
                  The sequence matters. If you skip straight to a large streaming request, you make auth, routing, and worker issues much harder to isolate.
                </div>
              </div>
              <div className="docs-step-grid">
                {steps.map((step) => (
                  <div key={step.number} className="docs-step-card">
                    <div className="docs-step-number">Step {step.number}</div>
                    <div className="docs-step-title">{step.title}</div>
                    <div className="docs-step-copy">{step.copy}</div>
                  </div>
                ))}
              </div>
            </section>

            <section className="docs-section tone" id="copy-run">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">COPY AND RUN</LabelText>
                  <h2 className="docs-section-title">Two commands before you do anything clever.</h2>
                </div>
                <div className="docs-section-copy">
                  First confirm the live model list. Then send one small chat request. If both pass, your auth path and routing path are in good shape.
                </div>
              </div>
              <div className="docs-code-grid">
                <div className="docs-code-panel">
                  <div className="docs-code-toolbar">
                    <LabelText as="div">1. LIST MODELS</LabelText>
                    <Badge>DISCOVERY</Badge>
                  </div>
                  <CodeExample code={modelsRequest} language="shell" />
                </div>
                <div className="docs-code-panel">
                  <div className="docs-code-toolbar">
                    <LabelText as="div">2. SEND CHAT</LabelText>
                    <Badge>UNARY</Badge>
                  </div>
                  <CodeExample code={chatRequest} language="shell" />
                </div>
              </div>
            </section>

            <section className="docs-section tone" id="sdk-examples">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">SDK EXAMPLES</LabelText>
                  <h2 className="docs-section-title">Drop-in with any OpenAI-compatible SDK.</h2>
                </div>
                <div className="docs-section-copy">
                  Point your existing OpenAI SDK at your Infera gateway. Change <code className="docs-inline-code">base_url</code> and swap in your <code className="docs-inline-code">inf_...</code> key — nothing else needs to change.
                </div>
              </div>
              <div className="docs-code-grid">
                <div className="docs-code-panel">
                  <div className="docs-code-toolbar">
                    <LabelText as="div">PYTHON</LabelText>
                    <Badge>openai SDK</Badge>
                  </div>
                  <CodeExample code={pythonSdkExample} language="python" />
                </div>
                <div className="docs-code-panel">
                  <div className="docs-code-toolbar">
                    <LabelText as="div">JAVASCRIPT / NODE</LabelText>
                    <Badge>openai SDK</Badge>
                  </div>
                  <CodeExample code={jsSdkExample} language="typescript" />
                </div>
              </div>
            </section>

            <section className="docs-section" id="streaming">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">STREAMING</LabelText>
                  <h2 className="docs-section-title">Only after the unary path is green.</h2>
                </div>
                <div className="docs-section-copy">
                  Streaming should be a second check, not the first. That keeps transport problems separate from auth or model-routing problems.
                </div>
              </div>
              <div className="docs-card-grid">
                <div className="docs-code-panel">
                  <LabelText as="div">STREAM REQUEST</LabelText>
                  <CodeExample code={streamRequest} language="shell" />
                </div>
                <div className="docs-card">
                  <LabelText as="div">EXPECT</LabelText>
                  <div className="docs-list">
                    <div>Content arrives in SSE chunks, not one JSON body.</div>
                    <div>The stream ends with <span className="docs-inline-code">data: [DONE]</span>.</div>
                    <div>Clients should keep reading until that final marker appears.</div>
                  </div>
                </div>
              </div>
            </section>

            <section className="docs-section tone" id="failures">
              <div className="docs-section-head">
                <div>
                  <LabelText as="div">FAILURE CHECKS</LabelText>
                  <h2 className="docs-section-title">What to verify first when a request fails.</h2>
                </div>
                <div className="docs-section-copy">
                  Keep the diagnosis tight. Most first-request failures are not exotic. They are almost always auth, a bad model id, or no healthy worker for the requested model.
                </div>
              </div>
              <div className="docs-card-grid">
                <div className="docs-card">
                  <LabelText as="div">CHECKLIST</LabelText>
                  <div className="docs-list">
                    {failureChecks.map((item) => (
                      <div key={item}>{item}</div>
                    ))}
                  </div>
                </div>
                <div className="docs-card">
                  <LabelText as="div">NEXT STEP</LabelText>
                  <div style={{ marginTop: '0.7rem', color: 'var(--text-secondary)', lineHeight: 1.7 }}>
                    If the first request works, move to the full docs for supported fields, compatibility boundaries, and SDK examples.
                  </div>
                  <div className="docs-actions" style={{ marginTop: '1rem' }}>
                    <Link className="btn-primary" to="/docs" style={{ textDecoration: 'none' }}>
                      OPEN API DOCS
                    </Link>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
        </main>
    </AppShell>
  );
}
