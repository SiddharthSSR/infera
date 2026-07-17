const API_BASE = '';

export interface CostMetrics {
  currency: 'USD';
  cost_usd: number;
  cost_per_request_usd: number;
  cost_per_token_usd: number;
  cost_per_1m_tokens_usd: number;
  costed_requests: number;
  costed_tokens: number;
  exact_requests: number;
  estimated_requests: number;
  unavailable_requests: number;
}

export interface AuditUsageRow {
  bucket_start: string;
  workspace_id: string;
  key_id: string;
  attempts?: number;
  requests: number;
  tokens: number;
  exact_requests?: number;
  estimated_requests?: number;
  exact_tokens?: number;
  estimated_tokens?: number;
  successes: number;
  errors: number;
  cost?: CostMetrics;
}

export interface AuditUsageResponse {
  bucket: 'day' | 'hour';
  start: string;
  end: string;
  rows: AuditUsageRow[];
  reconciliation?: {
    status: 'ok' | 'mismatch';
    discrepancies: string[];
  };
}

export async function readResponseError(response: Response, fallbackMessage: string): Promise<string> {
  const contentType = response.headers?.get?.('content-type') || '';
  const statusCode = response.status ?? 0;
  const statusText = response.statusText ?? '';
  const status = `${statusCode || 'unknown'} ${statusText}`.trim();
  let detail = '';

  try {
    if (contentType.includes('application/json') || (!contentType && typeof response.json === 'function')) {
      const payload = await response.json();
      detail =
        payload?.error?.message ||
        payload?.message ||
        (payload ? JSON.stringify(payload) : '');
    } else {
      detail = (await response.text()).trim();
    }
  } catch {
    try {
      detail = (await response.text()).trim();
    } catch {
      detail = '';
    }
  }

  if (!detail) {
    return `${fallbackMessage} (${status})`;
  }
  return `${fallbackMessage} (${status}): ${detail}`;
}

export async function readResponseMessage(response: Response, fallbackMessage: string): Promise<string> {
  const contentType = response.headers?.get?.('content-type') || '';

  try {
    if (contentType.includes('application/json') || (!contentType && typeof response.json === 'function')) {
      const payload = await response.json();
      const detail = payload?.error?.message || payload?.message;
      if (typeof detail === 'string' && detail.trim()) {
        return detail.trim();
      }
    } else {
      const detail = (await response.text()).trim();
      if (detail) {
        return detail;
      }
    }
  } catch {
    try {
      const detail = (await response.text()).trim();
      if (detail) {
        return detail;
      }
    } catch {
      // Fall through to the provided fallback.
    }
  }

  return fallbackMessage;
}

export async function authFetch(url: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(url, {
    ...init,
    credentials: 'include',
  });

  if (response.status === 401) {
    window.dispatchEvent(new Event('auth-expired'));
  }

  return response;
}

export { API_BASE };
