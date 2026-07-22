import { Link } from 'react-router-dom';
import { publicAnalytics } from '../../lib/publicAnalytics';

const walkthroughSteps = [
  {
    number: '01',
    eyebrow: 'Models / discovery',
    title: 'Start with the model ID the workspace exposes.',
    description: 'Models separates registry availability from serving state. Confirm the request ID against GET /v1/models before you build around it.',
    link: '/getting-started#copy-run',
    linkLabel: 'Run model discovery',
    resource: 'quickstart' as const,
    visual: 'models' as const,
  },
  {
    number: '02',
    eyebrow: 'Nodes / readiness',
    title: 'Wait for worker and model readiness.',
    description: 'A node is ready to serve only after the worker connects, reports a recent healthy heartbeat, and has the assigned model loaded.',
    link: '/getting-started#failures',
    linkLabel: 'Review readiness checks',
    resource: 'quickstart' as const,
    visual: 'worker' as const,
  },
  {
    number: '03',
    eyebrow: 'Playground / request',
    title: 'Exercise the same route your client will call.',
    description: 'Choose a live model and run one small unary prompt in Playground. Promote the request to streaming only after the first response succeeds.',
    link: '/docs#quickstart',
    linkLabel: 'Read the request contract',
    resource: 'api_docs' as const,
    visual: 'playground' as const,
  },
  {
    number: '04',
    eyebrow: 'Logs / inspection',
    title: 'Narrow the failure boundary.',
    description: 'Filter the operator console by level, source, or message to distinguish gateway, worker, and runtime symptoms before changing the client.',
    link: '/getting-started#failures',
    linkLabel: 'Open the failure runbook',
    resource: 'quickstart' as const,
    visual: 'logs' as const,
  },
] as const;

const useCases = [
  {
    label: 'OpenAI migration',
    title: 'Change the endpoint, then prove the route.',
    description: 'Keep the OpenAI client flow, discover a live model ID, validate one unary completion, and add SSE streaming last.',
  },
  {
    label: 'Self-hosted operations',
    title: 'Treat readiness as a chain, not a badge.',
    description: 'Check the node, worker heartbeat, loaded model, and inference verification before directing traffic to self-hosted capacity.',
  },
  {
    label: 'Failure diagnosis',
    title: 'Work backward from the observed symptom.',
    description: 'Use request output, filtered operator logs, and deployment history to separate access, routing, worker, and model-load failures.',
  },
] as const;

function ModelsVisual() {
  return (
    <div className="walkthrough-ui walkthrough-ui-models" aria-hidden="true">
      <div className="walkthrough-ui-toolbar"><span>MODELS</span><span>REGISTRY + SERVING</span></div>
      <div className="walkthrough-ui-search">SEARCH MODELS <span>mistral</span></div>
      <div className="walkthrough-model-row">
        <div className="walkthrough-model-mark">MI</div>
        <div><strong>Mistral 7B Instruct v0.3</strong><code>mistralai/Mistral-7B-Instruct-v0.3</code></div>
        <span className="walkthrough-chip">AVAILABLE</span>
      </div>
      <div className="walkthrough-ui-note"><span>DISCOVERY SOURCE</span><code>GET /v1/models</code></div>
    </div>
  );
}

function WorkerVisual() {
  return (
    <div className="walkthrough-ui walkthrough-ui-worker" aria-hidden="true">
      <div className="walkthrough-ui-toolbar"><span>NODES</span><span>READINESS</span></div>
      <div className="walkthrough-readiness-row"><span>01</span><strong>WORKER CONNECTED</strong><i>✓</i></div>
      <div className="walkthrough-readiness-row"><span>02</span><strong>HEARTBEAT RECENT</strong><i>✓</i></div>
      <div className="walkthrough-readiness-row"><span>03</span><strong>MODEL LOADED</strong><i>✓</i></div>
      <div className="walkthrough-readiness-result"><span>SERVING STATE</span><strong>SERVING VERIFIED</strong></div>
    </div>
  );
}

function PlaygroundVisual() {
  return (
    <div className="walkthrough-ui walkthrough-ui-playground" aria-hidden="true">
      <div className="walkthrough-ui-toolbar"><span>PLAYGROUND</span><span>CHAT</span></div>
      <div className="walkthrough-playground-grid">
        <div className="walkthrough-playground-settings">
          <span>ACTIVE MODEL</span>
          <strong>Mistral-7B-Instruct-v0.3</strong>
          <span>MODE</span>
          <strong>UNARY FIRST</strong>
        </div>
        <div className="walkthrough-playground-request">
          <span>USER PROMPT</span>
          <p>Send one small request to verify auth, routing, and model availability.</p>
          <b>RUN INFERENCE →</b>
        </div>
      </div>
    </div>
  );
}

function LogsVisual() {
  return (
    <div className="walkthrough-ui walkthrough-ui-logs" aria-hidden="true">
      <div className="walkthrough-ui-toolbar"><span>LOGS</span><span>OPERATOR CONSOLE</span></div>
      <div className="walkthrough-log-filters"><span>INFO</span><span>WARN</span><span>ERROR</span><span>ALL SOURCES</span></div>
      <div className="walkthrough-log-row"><time>12:08:41</time><b>INFO</b><code>GATEWAY</code><span>Request accepted: model inference</span></div>
      <div className="walkthrough-log-row"><time>12:08:42</time><b>INFO</b><code>WORKER</code><span>Worker heartbeat received</span></div>
      <div className="walkthrough-log-row walkthrough-log-row-error"><time>12:08:43</time><b>ERROR</b><code>INFERENCE</code><span>Inspect runtime error details</span></div>
    </div>
  );
}

const visuals = {
  models: <ModelsVisual />,
  worker: <WorkerVisual />,
  playground: <PlaygroundVisual />,
  logs: <LogsVisual />,
};

export function ProductWalkthrough() {
  return (
    <section className="landing-section landing-product-walkthrough" id="product" aria-labelledby="walkthrough-heading">
      <div className="landing-section-heading">
        <div>
          <span className="landing-meta">Real product walkthrough</span>
          <h2 id="walkthrough-heading">Follow one request from model to evidence.</h2>
        </div>
        <p>Four source-backed views show the operator path. The interface excerpts use repository labels and example content; they do not represent live capacity or customer data.</p>
      </div>

      <ol className="walkthrough-steps" aria-label="Infera operator walkthrough">
        {walkthroughSteps.map((step) => (
          <li key={step.number} className="walkthrough-step">
            <div className="walkthrough-step-copy">
              <div className="walkthrough-step-index" aria-hidden="true">{step.number}</div>
              <span className="landing-meta">{step.eyebrow}</span>
              <h3>{step.title}</h3>
              <p>{step.description}</p>
              <Link
                to={step.link}
                onClick={() => publicAnalytics.track('public_resource_opened', { resource: step.resource, source: 'landing' })}
              >{step.linkLabel} →</Link>
            </div>
            <figure className="walkthrough-figure">
              {visuals[step.visual]}
              <figcaption>{step.eyebrow} interface excerpt. Example content only; no live status is implied.</figcaption>
            </figure>
          </li>
        ))}
      </ol>

      <div className="walkthrough-use-cases" id="migration" aria-labelledby="walkthrough-use-cases-heading">
        <div className="walkthrough-use-cases-heading">
          <span className="landing-meta">Where the loop helps</span>
          <h3 id="walkthrough-use-cases-heading">Three factual operating paths.</h3>
        </div>
        {useCases.map((useCase) => (
          <article key={useCase.label}>
            <h4 className="landing-meta">{useCase.label}</h4>
            <strong>{useCase.title}</strong>
            <p>{useCase.description}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
