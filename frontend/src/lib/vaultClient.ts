import type { CreateVaultModelInput, VaultModel, VaultModelFilter, VaultStats } from '../types';
import { API_BASE, authFetch, readResponseError } from './apiCore';

export async function fetchVaultModels(filters?: VaultModelFilter): Promise<{ models: VaultModel[]; count: number }> {
  const params = new URLSearchParams();
  if (filters?.family) params.set('family', filters.family);
  if (filters?.status) params.set('status', filters.status);
  if (filters?.search) params.set('search', filters.search);
  if (filters?.quantization) params.set('quantization', filters.quantization);
  if (filters?.tag) params.set('tag', filters.tag);

  const query = params.toString();
  const url = `${API_BASE}/api/vault/models${query ? '?' + query : ''}`;
  const response = await authFetch(url);
  if (!response.ok) throw new Error('Failed to fetch vault models');
  return response.json();
}

export async function fetchVaultStats(): Promise<VaultStats> {
  const response = await authFetch(`${API_BASE}/api/vault/stats`);
  if (!response.ok) throw new Error('Failed to fetch vault stats');
  return response.json();
}

export async function fetchVaultFamilies(): Promise<string[]> {
  const response = await authFetch(`${API_BASE}/api/vault/models/families`);
  if (!response.ok) throw new Error('Failed to fetch families');
  const data = await response.json();
  return data.families;
}

export async function registerVaultModel(model: CreateVaultModelInput): Promise<VaultModel> {
  const response = await authFetch(`${API_BASE}/api/vault/models`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(model),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Registration failed'));
  }
  return response.json();
}

export async function deleteVaultModel(id: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/vault/models/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Delete failed'));
  }
}
