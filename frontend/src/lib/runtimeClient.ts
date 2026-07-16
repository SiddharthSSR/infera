import type { Model, Stats, Worker } from '../types';
import { API_BASE, authFetch } from './apiCore';

export async function fetchWorkers(): Promise<Worker[]> {
  const response = await authFetch(`${API_BASE}/api/workers`);
  if (!response.ok) throw new Error('Failed to fetch workers');
  const data = await response.json();
  return data.workers;
}

export async function fetchModels(): Promise<Model[]> {
  const response = await authFetch(`${API_BASE}/v1/models`);
  if (!response.ok) throw new Error('Failed to fetch models');
  const data = await response.json();
  return data.data;
}

export async function fetchStats(): Promise<Stats> {
  const response = await authFetch(`${API_BASE}/api/stats`);
  if (!response.ok) throw new Error('Failed to fetch stats');
  return response.json();
}
