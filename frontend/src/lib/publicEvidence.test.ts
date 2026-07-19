/// <reference types="vitest/globals" />
import { describe, expect, it } from 'vitest';
import {
  fingerprintPublicEvidence,
  publicEvidenceLinks,
  publicEvidenceReview,
} from './publicEvidence';

describe('public evidence review metadata', () => {
  it('requires evidence changes to update the public review metadata', () => {
    expect(fingerprintPublicEvidence(publicEvidenceLinks)).toBe(publicEvidenceReview.evidenceFingerprint);
  });
});
