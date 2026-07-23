export interface DesignPartnerRequest {
  workEmail: string;
  company: string;
  role: string;
  currentInferenceStack: string;
  evaluationGoal: string;
}

interface DesignPartnerRequestEnvironment {
  readonly VITE_DESIGN_PARTNER_REQUEST_ENDPOINT?: string;
}

export type PublicAcquisitionAction = 'evaluate_deployment_fit' | 'request_design_partner_access';

export interface PublicAcquisitionTarget {
  action: PublicAcquisitionAction;
  path: '/evaluation' | '/request-access';
}

interface SubmitDesignPartnerRequestOptions {
  endpoint: string;
  fetcher?: typeof fetch;
}

export function getDesignPartnerRequestEndpoint(
  environment: DesignPartnerRequestEnvironment,
): string | undefined {
  const endpoint = environment.VITE_DESIGN_PARTNER_REQUEST_ENDPOINT?.trim();
  if (!endpoint) {
    return undefined;
  }

  if (endpoint.startsWith('/') && !endpoint.startsWith('//')) {
    return endpoint;
  }

  try {
    const url = new URL(endpoint);
    return url.protocol === 'https:' ? url.toString() : undefined;
  } catch {
    return undefined;
  }
}

export function getPublicAcquisitionTarget(endpoint?: string): PublicAcquisitionTarget {
  const configuredEndpoint = getDesignPartnerRequestEndpoint({
    VITE_DESIGN_PARTNER_REQUEST_ENDPOINT: endpoint,
  });

  return configuredEndpoint
    ? { action: 'request_design_partner_access', path: '/request-access' }
    : { action: 'evaluate_deployment_fit', path: '/evaluation' };
}

export async function submitDesignPartnerRequest(
  request: DesignPartnerRequest,
  { endpoint, fetcher = fetch }: SubmitDesignPartnerRequestOptions,
): Promise<void> {
  const response = await fetcher(endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
    credentials: 'omit',
    referrerPolicy: 'no-referrer',
  });

  if (!response.ok) {
    throw new Error(`Design-partner request endpoint returned ${response.status}`);
  }
}

const viteEnvironment = (
  import.meta as ImportMeta & { readonly env?: DesignPartnerRequestEnvironment }
).env ?? {};

export const designPartnerRequestEndpoint = getDesignPartnerRequestEndpoint(viteEnvironment);
