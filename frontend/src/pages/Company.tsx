import { Link } from 'react-router-dom';
import { AppShell, PublicFooter, PublicNav, TrustStatus } from '../components/shared';
import { publicEvidenceLinks } from '../lib/publicEvidence';

const companyRecords = [
  ['Legal company identity', 'Not published'],
  ['Founders and team', 'Not published'],
  ['Office or registered address', 'Not published'],
  ['Dedicated company contact', 'Not published'],
  ['Design-partner intake', 'Not published'],
] as const;

export function Company() {
  return (
    <AppShell variant="public" className="trust-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="COMPANY RECORD" />

      <main id="main-content">
        <header className="trust-page-header">
          <span className="landing-meta">Company / public record</span>
          <h1>A product thesis, without an invented company story.</h1>
          <p>
            Infera is building an inference gateway and control plane for infrastructure teams that want to run open
            models behind an OpenAI-compatible client flow. Public legal, team, and contact details have not been approved for publication.
          </p>
        </header>

        <section className="trust-section trust-section-tone" aria-labelledby="why-heading">
          <div className="trust-section-heading">
            <div>
              <span className="landing-meta">Why this product</span>
              <h2 id="why-heading">Migration first. Operations close behind.</h2>
            </div>
            <p>
              The product starts with a narrow compatibility boundary: discover live models, send chat completions,
              and add streaming after the first request works. The dashboard keeps models, nodes, logs, keys, and workspace controls near that serving path.
            </p>
          </div>
          <div className="trust-principles">
            <article>
              <span className="landing-meta">01 / Entry point</span>
              <h3>Keep the client workflow familiar.</h3>
              <p>Change the endpoint and credential context before changing application architecture.</p>
            </article>
            <article>
              <span className="landing-meta">02 / Operating model</span>
              <h3>Expose the serving path.</h3>
              <p>Put model availability, runtime capacity, request traces, and access controls in one workspace.</p>
            </article>
            <article>
              <span className="landing-meta">03 / Trust posture</span>
              <h3>Label what is not ready.</h3>
              <p>Do not imply customers, scale, certifications, or corporate maturity that public evidence cannot support.</p>
            </article>
          </div>
        </section>

        <section className="trust-section" aria-labelledby="company-record-heading">
          <div className="trust-section-heading">
            <div>
              <span className="landing-meta">Publication status</span>
              <h2 id="company-record-heading">Company details remain a named blocker.</h2>
            </div>
            <p>These omissions prevent a complete company and design-partner contact surface. They are shown explicitly instead of being filled with assumptions.</p>
          </div>
          <dl className="company-record-grid">
            {companyRecords.map(([label, status]) => (
              <div key={label}>
                <dt>{label}</dt>
                <dd><TrustStatus tone="unavailable">{status}</TrustStatus></dd>
              </div>
            ))}
          </dl>
          <aside className="trust-caution">
            <strong>Repository-scoped questions only</strong>
            <p>
              Public GitHub issues are available for project questions. They are not a private contact channel and should not contain credentials, personal data, or vulnerability details.
            </p>
            <a className="trust-evidence-link" href={publicEvidenceLinks.issues} target="_blank" rel="noreferrer">
              Open GitHub issues<span className="sr-only"> (opens in a new tab)</span>
            </a>
          </aside>
        </section>

        <section className="landing-final-cta" aria-labelledby="company-cta-heading">
          <div>
            <span className="landing-meta">Evaluate the interface</span>
            <h2 id="company-cta-heading">Start with the evidence the product can provide.</h2>
          </div>
          <Link className="landing-button" to="/getting-started">Run the quickstart</Link>
        </section>
      </main>

      <PublicFooter />
    </AppShell>
  );
}
