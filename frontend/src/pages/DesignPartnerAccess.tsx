import { FormEvent, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { AppShell, PublicFooter, PublicNav } from '../components/shared';
import {
  designPartnerRequestEndpoint,
  getDesignPartnerRequestEndpoint,
  submitDesignPartnerRequest,
  type DesignPartnerRequest,
} from '../lib/designPartnerRequest';
import { publicAnalytics } from '../lib/publicAnalytics';

type FieldName = keyof DesignPartnerRequest | 'consent';
type FieldErrors = Partial<Record<FieldName, string>>;

const initialRequest: DesignPartnerRequest = {
  workEmail: '',
  company: '',
  role: '',
  currentInferenceStack: '',
  evaluationGoal: '',
};

function validateRequest(request: DesignPartnerRequest, consent: boolean): FieldErrors {
  const errors: FieldErrors = {};
  if (!/^\S+@\S+\.\S+$/.test(request.workEmail.trim())) errors.workEmail = 'Enter a valid work email address.';
  if (!request.company.trim()) errors.company = 'Enter your company or organization.';
  if (!request.role.trim()) errors.role = 'Enter your role.';
  if (!request.currentInferenceStack.trim()) errors.currentInferenceStack = 'Describe your current inference stack.';
  if (!request.evaluationGoal.trim()) errors.evaluationGoal = 'Describe what you want to evaluate.';
  if (!consent) errors.consent = 'Confirm that Infera may use these details to respond to this request.';
  return errors;
}

export interface DesignPartnerAccessProps {
  endpoint?: string;
}

export function DesignPartnerAccess({ endpoint = designPartnerRequestEndpoint }: DesignPartnerAccessProps) {
  const configuredEndpoint = getDesignPartnerRequestEndpoint({
    VITE_DESIGN_PARTNER_REQUEST_ENDPOINT: endpoint,
  });
  const [request, setRequest] = useState(initialRequest);
  const [consent, setConsent] = useState(false);
  const [errors, setErrors] = useState<FieldErrors>({});
  const [submitState, setSubmitState] = useState<'idle' | 'submitting' | 'failed' | 'succeeded'>('idle');
  const started = useRef(false);
  const errorSummary = useRef<HTMLDivElement>(null);
  const successSummary = useRef<HTMLDivElement>(null);

  const recordStart = () => {
    if (!started.current) {
      started.current = true;
      publicAnalytics.track('design_partner_request_started', { source: 'request_access' });
    }
  };

  const updateField = (field: keyof DesignPartnerRequest, value: string) => {
    setRequest((current) => ({ ...current, [field]: value }));
    setErrors((current) => {
      const next = { ...current };
      delete next[field];
      return next;
    });
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    recordStart();
    const nextErrors = validateRequest(request, consent);
    if (Object.keys(nextErrors).length > 0) {
      setErrors(nextErrors);
      setSubmitState('idle');
      publicAnalytics.track('design_partner_request_submitted', { outcome: 'validation_failed' });
      window.setTimeout(() => errorSummary.current?.focus(), 0);
      return;
    }

    if (!configuredEndpoint) {
      setSubmitState('failed');
      publicAnalytics.track('design_partner_request_submitted', { outcome: 'configuration_missing' });
      return;
    }

    setSubmitState('submitting');
    try {
      await submitDesignPartnerRequest({
        workEmail: request.workEmail.trim(),
        company: request.company.trim(),
        role: request.role.trim(),
        currentInferenceStack: request.currentInferenceStack.trim(),
        evaluationGoal: request.evaluationGoal.trim(),
      }, { endpoint: configuredEndpoint });
      setSubmitState('succeeded');
      publicAnalytics.track('design_partner_request_submitted', { outcome: 'succeeded' });
      window.setTimeout(() => successSummary.current?.focus(), 0);
    } catch {
      setSubmitState('failed');
      publicAnalytics.track('design_partner_request_submitted', { outcome: 'delivery_failed' });
    }
  };

  const fieldError = (field: FieldName) => errors[field] ? `${field}-error` : undefined;

  return (
    <AppShell variant="public" className="request-access-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="DESIGN-PARTNER ACCESS" intakeEndpoint={configuredEndpoint} />

      <main id="main-content">
        <header className="request-access-header">
          <div>
            <span className="landing-meta">Evaluation / design partner</span>
            <h1>{configuredEndpoint ? 'Bring one inference problem. Evaluate it with us.' : 'Evaluate the fit. Keep your details with you.'}</h1>
          </div>
          <div className="request-access-intro">
            <p>
              {configuredEndpoint
                ? 'Tell us where your current inference path is getting in the way and what you need to verify. This is a request for a design-partner conversation, not a paid subscription or a promise of access.'
                : 'Design-partner intake is not configured. Use the public evaluation guide to assess deployment fit without sending us contact details or evaluation context.'}
            </p>
            <p className="request-access-boundary"><strong>Do not include</strong> API keys, credentials, prompts, model output, customer data, or other secrets.</p>
          </div>
        </header>

        <section
          className="request-access-layout"
          aria-labelledby={configuredEndpoint ? 'request-form-heading' : 'request-unavailable-heading'}
        >
          <aside className="request-access-steps" aria-label="What happens next">
            <span className="landing-meta">{configuredEndpoint ? 'The path' : 'Available now'}</span>
            {configuredEndpoint ? (
              <ol>
                <li><span>01</span><div><strong>Describe</strong><p>Share the stack and evaluation goal—nothing sensitive.</p></div></li>
                <li><span>02</span><div><strong>Review</strong><p>The configured intake owner reviews the fit and context.</p></div></li>
                <li><span>03</span><div><strong>Respond</strong><p>If there is a useful next step, they reply to your work email.</p></div></li>
              </ol>
            ) : (
              <ol>
                <li><span>01</span><div><strong>Evaluate</strong><p>Review the deployment-fit criteria and record your own evidence.</p></div></li>
                <li><span>02</span><div><strong>Run</strong><p>Use the migration quickstart with an existing workspace.</p></div></li>
                <li><span>03</span><div><strong>Verify</strong><p>Inspect the public trust and API records before adoption.</p></div></li>
              </ol>
            )}
            <p className="request-access-note">Already have workspace access? <Link to="/getting-started">Run the migration quickstart.</Link></p>
          </aside>

          <div className="request-access-form-panel">
            {!configuredEndpoint ? (
              <section className="request-access-unavailable" aria-labelledby="request-unavailable-heading">
                <span className="landing-meta">Intake unavailable</span>
                <h2 id="request-unavailable-heading">Design-partner requests are not open yet.</h2>
                <p>
                  An approved delivery endpoint has not been configured, so this page does not collect or submit contact details.
                  No request has been sent or saved.
                </p>
                <p>
                  You can still evaluate deployment fit from the public evidence and run the quickstart if you already have workspace access.
                </p>
                <div className="request-access-unavailable-actions">
                  <Link className="landing-button landing-button-primary" to="/evaluation">Evaluate deployment fit</Link>
                  <Link className="landing-button landing-button-secondary" to="/getting-started">Run the quickstart</Link>
                </div>
              </section>
            ) : submitState === 'succeeded' ? (
              <div className="request-access-success" role="status" tabIndex={-1} ref={successSummary}>
                <span className="landing-meta">Request received</span>
                <h2>Thank you. Your evaluation context was delivered.</h2>
                <p>A response is not guaranteed. You can continue evaluating the public API surface while the request is reviewed.</p>
                <Link className="landing-button landing-button-primary" to="/getting-started">Run the quickstart</Link>
              </div>
            ) : (
              <form noValidate aria-busy={submitState === 'submitting'} onSubmit={(event) => void handleSubmit(event)} onFocusCapture={recordStart}>
                <div className="request-access-form-heading">
                  <div><span className="landing-meta">Request access</span><h2 id="request-form-heading">Five details. No credentials.</h2></div>
                  <span className="request-required-note">All fields required</span>
                </div>

                {Object.keys(errors).length > 0 ? (
                  <div className="request-error-summary" role="alert" tabIndex={-1} ref={errorSummary}>
                    <strong>Check the highlighted fields.</strong>
                    <span>Your request has not been sent.</span>
                  </div>
                ) : null}

                <div className="request-field-grid">
                  <label className="request-field">
                    <span>Work email</span>
                    <input aria-label="Work email" required type="email" name="workEmail" autoComplete="email" maxLength={160} value={request.workEmail} onChange={(event) => updateField('workEmail', event.target.value)} aria-invalid={Boolean(errors.workEmail)} aria-describedby={fieldError('workEmail')} />
                    {errors.workEmail ? <small id="workEmail-error">{errors.workEmail}</small> : null}
                  </label>
                  <label className="request-field">
                    <span>Company or organization</span>
                    <input aria-label="Company or organization" required name="company" autoComplete="organization" maxLength={120} value={request.company} onChange={(event) => updateField('company', event.target.value)} aria-invalid={Boolean(errors.company)} aria-describedby={fieldError('company')} />
                    {errors.company ? <small id="company-error">{errors.company}</small> : null}
                  </label>
                  <label className="request-field request-field-wide">
                    <span>Role</span>
                    <input aria-label="Role" required name="role" autoComplete="organization-title" maxLength={120} value={request.role} onChange={(event) => updateField('role', event.target.value)} aria-invalid={Boolean(errors.role)} aria-describedby={fieldError('role')} />
                    {errors.role ? <small id="role-error">{errors.role}</small> : null}
                  </label>
                  <label className="request-field request-field-wide">
                    <span>Current inference stack</span>
                    <textarea aria-label="Current inference stack" required name="currentInferenceStack" rows={3} maxLength={360} value={request.currentInferenceStack} onChange={(event) => updateField('currentInferenceStack', event.target.value)} aria-invalid={Boolean(errors.currentInferenceStack)} aria-describedby={['currentInferenceStack-hint', fieldError('currentInferenceStack')].filter(Boolean).join(' ')} />
                    <span className="request-field-hint" id="currentInferenceStack-hint">Tools and architecture only. Do not paste configuration or request data.</span>
                    {errors.currentInferenceStack ? <small id="currentInferenceStack-error">{errors.currentInferenceStack}</small> : null}
                  </label>
                  <label className="request-field request-field-wide">
                    <span>Evaluation goal</span>
                    <textarea aria-label="Evaluation goal" required name="evaluationGoal" rows={4} maxLength={600} value={request.evaluationGoal} onChange={(event) => updateField('evaluationGoal', event.target.value)} aria-invalid={Boolean(errors.evaluationGoal)} aria-describedby={['evaluationGoal-hint', fieldError('evaluationGoal')].filter(Boolean).join(' ')} />
                    <span className="request-field-hint" id="evaluationGoal-hint">Describe the outcome you need to evaluate, without prompts or customer information.</span>
                    {errors.evaluationGoal ? <small id="evaluationGoal-error">{errors.evaluationGoal}</small> : null}
                  </label>
                </div>

                <label className="request-consent">
                  <input required type="checkbox" checked={consent} onChange={(event) => { setConsent(event.target.checked); setErrors((current) => { const next = { ...current }; delete next.consent; return next; }); }} aria-invalid={Boolean(errors.consent)} aria-describedby={fieldError('consent')} />
                  <span>I understand these details will be sent to the administrator-configured intake destination and may be used to respond to this request.</span>
                </label>
                {errors.consent ? <small className="request-consent-error" id="consent-error">{errors.consent}</small> : null}

                {submitState === 'failed' ? <div className="request-submit-status" role="alert">We could not deliver this request. Your entries remain on this page; check them and try again later.</div> : null}

                <div className="request-submit-row">
                  <button className="landing-button landing-button-primary" type="submit" disabled={submitState === 'submitting'}>{submitState === 'submitting' ? 'Sending request…' : 'Request design-partner access'}</button>
                  <p>No payment is collected. Submission does not guarantee access, timing, or support.</p>
                </div>
              </form>
            )}
          </div>
        </section>
      </main>

      <PublicFooter intakeEndpoint={configuredEndpoint} />
    </AppShell>
  );
}
