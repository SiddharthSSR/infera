import type { CostSummary, GPUOffering, Instance, ProvisionRequest, ProviderStatus } from '../types';
import { parseCostSummary, parseInstancesResponse, parseOfferingsResponse, parseProvidersResponse } from './infrastructure';
import { API_BASE, authFetch, readResponseError } from './apiCore';

export async function fetchInstances(): Promise<Instance[]> {
  const response = await authFetch(`${API_BASE}/api/instances`);
  if (!response.ok) throw new Error('Failed to fetch instances');
  return parseInstancesResponse(await response.json()).instances;
}

export async function fetchOfferings(): Promise<GPUOffering[]> {
  const response = await authFetch(`${API_BASE}/api/offerings`);
  if (!response.ok) throw new Error('Failed to fetch offerings');
  return parseOfferingsResponse(await response.json()).offerings;
}

export async function fetchProviders(): Promise<ProviderStatus[]> {
  const response = await authFetch(`${API_BASE}/api/providers`);
  if (!response.ok) throw new Error('Failed to fetch providers');
  return parseProvidersResponse(await response.json()).providers;
}

export async function fetchCosts(): Promise<CostSummary> {
  const response = await authFetch(`${API_BASE}/api/costs`);
  if (!response.ok) throw new Error('Failed to fetch costs');
  return parseCostSummary(await response.json());
}

export async function provisionInstance(request: ProvisionRequest): Promise<Instance> {
  const response = await authFetch(`${API_BASE}/api/instances/provision`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Provisioning failed'));
  }

  const data = await response.json();
  return data.instance;
}

export async function terminateInstance(instanceId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/instances/${instanceId}`, {
    method: 'DELETE',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Termination failed'));
  }
}

export async function startInstance(instanceId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/instances/${instanceId}/start`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Start failed'));
  }
}

export async function stopInstance(instanceId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/instances/${instanceId}/stop`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Stop failed'));
  }
}
