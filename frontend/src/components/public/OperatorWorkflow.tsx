const operatorSteps = [
  {
    number: '01',
    phase: 'Prepare runtime',
    surfaces: 'Workspace · Nodes',
    title: 'Connect capacity before traffic.',
    description: 'Configure provider access in Workspace, then create a node with the model and runtime settings carried into its deployment attempt.',
    check: 'Boundary: provider credentials stay server-side and workspace-scoped.',
  },
  {
    number: '02',
    phase: 'Serve model',
    surfaces: 'Models · Nodes',
    title: 'Move from catalog to verified serving.',
    description: 'Use Models to check deployment readiness and start the deployment. Nodes keeps the attempt timeline, worker registration state, and inference verification together.',
    check: 'Boundary: a catalog entry is not the same as a healthy worker serving it.',
  },
  {
    number: '03',
    phase: 'Test request',
    surfaces: 'Playground · API',
    title: 'Exercise the workspace route.',
    description: 'Select a live model in Playground and run a real chat request, or call the same public route from the client you plan to migrate.',
    check: 'Boundary: verify unary first, then streaming through the final [DONE] marker.',
  },
  {
    number: '04',
    phase: 'Inspect and govern',
    surfaces: 'Logs · API Keys · Workspace',
    title: 'Separate runtime symptoms from access.',
    description: 'Correlate deployment history, gateway route logs, and authenticated audit usage, then review the key and workspace role that authorized the request.',
    check: 'Boundary: the current Logs screen is an operator console, not the durable audit ledger; /api/audit/usage is the source-backed usage surface.',
  },
] as const;

export function OperatorWorkflow() {
  return (
    <section className="landing-section landing-section-tone landing-operator-workflow" id="operator-loop" aria-labelledby="operator-loop-heading">
      <div className="landing-section-heading">
        <div>
          <span className="landing-meta">Operator loop</span>
          <h2 id="operator-loop-heading">Prepare. Serve. Test. Inspect.</h2>
        </div>
        <p>
          Compatibility gets a request through the front door. The control plane closes the loop around the runtime, serving state, verification request, and the access context behind it.
        </p>
      </div>

      <ol className="landing-operator-rail">
        {operatorSteps.map((step) => (
          <li key={step.number}>
            <div className="landing-operator-marker">
              <span>{step.number}</span>
            </div>
            <div className="landing-operator-phase">
              <span className="landing-meta">{step.phase}</span>
              <strong>{step.surfaces}</strong>
            </div>
            <div className="landing-operator-copy">
              <h3>{step.title}</h3>
              <p>{step.description}</p>
              <div className="landing-operator-check">{step.check}</div>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}
