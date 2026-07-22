import { Link } from 'react-router-dom';
import { AppShell, PublicFooter, PublicNav, TrustStatus } from '../components/shared';
import { PUBLIC_EVIDENCE_REVIEWED_ON, publicEvidenceLinks } from '../lib/publicEvidence';

const evidenceRecords = [
  {
    label: 'Source repository',
    status: 'Available',
    tone: 'available' as const,
    detail: 'The GitHub repository is public and its default branch is main.',
    href: publicEvidenceLinks.repository,
    linkLabel: 'Inspect repository',
  },
  {
    label: 'Project changelog',
    status: 'Available',
    tone: 'available' as const,
    detail: 'A frontend changelog is published in the repository.',
    href: publicEvidenceLinks.changelog,
    linkLabel: 'Read changelog',
  },
  {
    label: 'Publication decision record',
    status: 'Available',
    tone: 'available' as const,
    detail: 'A repository record names the exact owner decisions required before company, legal, privacy, security, and service claims can be published.',
    href: publicEvidenceLinks.publicationReadiness,
    linkLabel: 'Read decision record',
  },
  {
    label: 'Repository-wide software license',
    status: 'Owner decision required',
    tone: 'unavailable' as const,
    detail: 'The root README previously named MIT while the Python worker package declares Apache-2.0. No root license is published until scope, terms, and copyright text are approved.',
    href: publicEvidenceLinks.publicationReadiness,
    linkLabel: 'Review license decision',
  },
  {
    label: 'Public service status',
    status: 'Not published',
    tone: 'unavailable' as const,
    detail: 'No authoritative public status page is linked from the repository.',
  },
  {
    label: 'Security policy and private reporting',
    status: 'Not published',
    tone: 'unavailable' as const,
    detail: 'No SECURITY file or dedicated private vulnerability-reporting channel is published.',
  },
  {
    label: 'Company identity and design-partner intake',
    status: 'Configuration required',
    tone: 'unavailable' as const,
    detail: 'The request route is published, but no approved delivery endpoint, legal company profile, or team profile is available in the repository.',
  },
];

export function Trust() {
  return (
    <AppShell variant="public" className="trust-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="PUBLIC EVIDENCE" />

      <main id="main-content">
        <header className="trust-hero">
          <div className="trust-hero-copy">
            <span className="landing-meta">Trust / evidence ledger</span>
            <h1>Trust starts with what can be verified.</h1>
            <p>
              This record separates public evidence from material Infera has not published. It does not use
              customer logos, testimonials, certifications, service levels, or deployment guarantees as stand-ins for proof.
            </p>
            <div className="landing-actions">
              <Link className="landing-button landing-button-primary" to="/getting-started">Run the migration quickstart</Link>
              <a className="landing-button landing-button-secondary" href={publicEvidenceLinks.repository} target="_blank" rel="noreferrer">
                Inspect the source<span className="sr-only"> (opens in a new tab)</span>
              </a>
            </div>
          </div>
          <aside className="trust-review-note" aria-label="Evidence review scope">
            <span className="landing-meta">Evidence reviewed</span>
            <strong>{PUBLIC_EVIDENCE_REVIEWED_ON}</strong>
            <p>Repository files, configured Git remote, public GitHub metadata, and the public default branch.</p>
          </aside>
        </header>

        <section className="trust-section" aria-labelledby="evidence-heading">
          <div className="trust-section-heading">
            <div>
              <span className="landing-meta">Availability record</span>
              <h2 id="evidence-heading">A usable record of what exists today.</h2>
            </div>
            <p>“Not published” is a status, not a promise. These items remain blockers until an authoritative source exists.</p>
          </div>
          <dl className="trust-ledger">
            {evidenceRecords.map((record) => (
              <div key={record.label} className="trust-ledger-row">
                <dt>{record.label}</dt>
                <dd>
                  <TrustStatus tone={record.tone}>{record.status}</TrustStatus>
                  <p>{record.detail}</p>
                  {record.href ? (
                    <a className="trust-evidence-link" href={record.href} target="_blank" rel="noreferrer">
                      {record.linkLabel}<span className="sr-only"> (opens in a new tab)</span>
                    </a>
                  ) : null}
                </dd>
              </div>
            ))}
          </dl>
        </section>

        <section className="trust-section trust-section-tone" aria-labelledby="open-source-heading">
          <div className="trust-section-heading">
            <div>
              <span className="landing-meta">Source and license</span>
              <h2 id="open-source-heading">Public source. License terms need one owner decision.</h2>
            </div>
            <p>
              The repository can be inspected and its README includes contribution steps. The README's former MIT
              statement conflicts with Apache-2.0 metadata in the Python package, so this site does not infer reuse rights.
            </p>
          </div>
          <div className="trust-link-grid">
            <a href={publicEvidenceLinks.readme} target="_blank" rel="noreferrer">
              <span className="landing-meta">Root declaration</span>
              <strong>README license reconciliation note</strong>
              <span>Inspect README ↗</span>
            </a>
            <a href={publicEvidenceLinks.pythonPackaging} target="_blank" rel="noreferrer">
              <span className="landing-meta">Package declaration</span>
              <strong>Python worker Apache-2.0 metadata</strong>
              <span>Inspect package metadata ↗</span>
            </a>
            <a href={publicEvidenceLinks.publicationReadiness} target="_blank" rel="noreferrer">
              <span className="landing-meta">Decision boundary</span>
              <strong>Exact administrator approvals still required</strong>
              <span>Read decision record ↗</span>
            </a>
          </div>
          <div className="trust-link-grid trust-link-grid-secondary">
            <Link to="/security">
              <span className="landing-meta">Security</span>
              <strong>Implementation evidence and known gaps</strong>
              <span>Read the security record →</span>
            </Link>
            <Link to="/company">
              <span className="landing-meta">Company</span>
              <strong>Product thesis and public company gaps</strong>
              <span>Read the company record →</span>
            </Link>
          </div>
        </section>

        <section className="landing-final-cta" aria-labelledby="trust-cta-heading">
          <div>
            <span className="landing-meta">Primary path</span>
            <h2 id="trust-cta-heading">Verify the product with one compatible request.</h2>
          </div>
          <Link className="landing-button" to="/getting-started">Run the quickstart</Link>
        </section>
      </main>

      <PublicFooter />
    </AppShell>
  );
}
