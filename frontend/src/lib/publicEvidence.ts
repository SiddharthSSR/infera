export const publicEvidenceLinks = {
  repository: 'https://github.com/SiddharthSSR/infera',
  readme: 'https://github.com/SiddharthSSR/infera/blob/main/README.md',
  pythonPackaging: 'https://github.com/SiddharthSSR/infera/blob/main/python/pyproject.toml',
  publicationReadiness: 'https://github.com/SiddharthSSR/infera/blob/main/docs/trust/publication-readiness.md',
  issues: 'https://github.com/SiddharthSSR/infera/issues',
  changelog: 'https://github.com/SiddharthSSR/infera/blob/main/frontend/CHANGELOG.md',
  compatibility: 'https://github.com/SiddharthSSR/infera/blob/main/docs/openai/COMPATIBILITY.md',
  providerConformance: 'https://github.com/SiddharthSSR/infera/blob/main/docs/providers/CONFORMANCE.md',
  modularBackend: 'https://github.com/SiddharthSSR/infera/blob/main/docs/MODULAR_INFERENCE_BACKEND.md',
  localCompose: 'https://github.com/SiddharthSSR/infera/blob/main/docker-compose.yml',
  productionCompose: 'https://github.com/SiddharthSSR/infera/blob/main/docker-compose.prod.yml',
  deploymentRecovery: 'https://github.com/SiddharthSSR/infera/blob/main/docs/operations/deployment-recovery.md',
  sharedAuditLedger: 'https://github.com/SiddharthSSR/infera/blob/main/docs/operations/shared-audit-ledger.md',
  ingressConfiguration: 'https://github.com/SiddharthSSR/infera/blob/main/deploy/docker/nginx.conf',
} as const;

export function fingerprintPublicEvidence(links: typeof publicEvidenceLinks): string {
  const canonicalEvidence = Object.entries(links)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key}:${value}`)
    .join('\n');
  let hash = 2166136261;

  for (let index = 0; index < canonicalEvidence.length; index += 1) {
    hash ^= canonicalEvidence.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }

  return (hash >>> 0).toString(16).padStart(8, '0');
}

export const publicEvidenceReview = {
  reviewedOn: '22 July 2026',
  evidenceFingerprint: '30366383',
} as const;

export const PUBLIC_EVIDENCE_REVIEWED_ON = publicEvidenceReview.reviewedOn;
