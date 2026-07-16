/**
 * Shared formatting functions used across multiple pages.
 * Centralizes date, number, and display formatting to eliminate duplication.
 */

/* ------------------------------------------------------------------ */
/*  Date / time formatting                                             */
/* ------------------------------------------------------------------ */

/** Full date: "Apr 8, 2026" */
export function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr) return 'Never';
  try {
    return new Date(dateStr).toLocaleDateString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    });
  } catch {
    return dateStr;
  }
}

/** Date + time: "Apr 8, 2026, 3:42 PM" */
export function formatDateTime(dateStr?: string | null): string {
  if (!dateStr) return 'Never';
  try {
    return new Date(dateStr).toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    });
  } catch {
    return dateStr;
  }
}

/** Short timestamp: "Apr 8, 3:42 PM" — used for attempt times, verification times */
export function formatShortTimestamp(value?: string): string | null {
  if (!value) return null;
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return value;
  return new Date(timestamp).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
}

/* ------------------------------------------------------------------ */
/*  Number formatting                                                  */
/* ------------------------------------------------------------------ */

/** Locale-formatted integer: "1,234,567" */
export function formatCount(value: number): string {
  return new Intl.NumberFormat('en-US').format(value);
}

/** Compact number: "1.2M", "3.4K", "42" */
export function formatCompactCount(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

/** Percentage display: "85%" or "100%+" for infinity */
export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '100%+';
  return `${Math.round(value * 100)}%`;
}

/** Clamp ratio to 0-100 for progress bars */
export function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 100;
  if (value < 0) return 0;
  return Math.min(value * 100, 100);
}

/** used / limit ratio, handling null and zero limits */
export function usageRatio(used: number, limit?: number | null): number {
  if (limit == null) return 0;
  if (limit <= 0) return used > 0 ? Number.POSITIVE_INFINITY : 1;
  return used / limit;
}

/** Parse a user-entered limit that may be empty (→null) or invalid (→NaN) */
export function parseNullableLimit(value: string): number | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) return NaN;
  return parsed;
}

/* ------------------------------------------------------------------ */
/*  Latency formatting                                                 */
/* ------------------------------------------------------------------ */

/** Format ms latency: "412ms" or "1.23s" */
export function formatLatency(latencyMs?: number): string | null {
  if (latencyMs == null) return null;
  if (latencyMs < 1000) return `${latencyMs}ms`;
  return `${(latencyMs / 1000).toFixed(2)}s`;
}

/** Verification timestamp + latency: "Apr 8, 3:42 PM in 412ms" */
export function formatVerificationMeta(verifiedAt?: string, latencyMs?: number): string | null {
  if (!verifiedAt) return null;
  const label = formatShortTimestamp(verifiedAt);
  if (!label) return null;
  if (latencyMs == null) return label;
  return `${label} in ${formatLatency(latencyMs)}`;
}

/* ------------------------------------------------------------------ */
/*  Date range helpers                                                 */
/* ------------------------------------------------------------------ */

/** Current calendar month range for usage queries */
export function monthRange(): { start: string; end: string } {
  const now = new Date();
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1, 0, 0, 0, 0));
  return { start: start.toISOString(), end: now.toISOString() };
}
