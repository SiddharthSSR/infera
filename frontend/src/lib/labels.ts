/**
 * Shared label/mapping functions for status, role, and display values.
 * Centralizes string transformations used across multiple pages.
 */

import type { ProviderCapabilities, ProviderStatus, ProviderType, GPUType } from '../types';
import type { DeploymentTimelineStep } from './deploymentHistory';

/* ------------------------------------------------------------------ */
/*  Generic string transforms                                          */
/* ------------------------------------------------------------------ */

/** "some_value" → "SOME VALUE" */
export function snakeToUpperLabel(value: string): string {
  return value.replace(/_/g, ' ').toUpperCase();
}

/** Truncate with ellipsis */
export function truncate(text: string, maxLength: number): string {
  return text.length > maxLength ? text.slice(0, maxLength) + '...' : text;
}

/* ------------------------------------------------------------------ */
/*  API key / principal labels                                         */
/* ------------------------------------------------------------------ */

export function principalLabel(principalType?: string): string {
  return principalType === 'service_account' ? 'SERVICE ACCOUNT' : 'HUMAN';
}

export function roleLabel(role: string): string {
  return role.replace(/_/g, ' ').toUpperCase();
}

export function roleDescription(role: string, principalType: 'human' | 'service_account'): string {
  switch (role) {
    case 'admin':
      return principalType === 'human'
        ? 'Full workspace administration, membership, key, quota, and provider management.'
        : 'Not available for service accounts.';
    case 'operator':
      return 'Infrastructure operations and deployment control for this workspace.';
    case 'developer':
      return 'Model and product development access within this workspace.';
    case 'billing':
      return 'Quota, usage, and billing visibility for this workspace.';
    case 'read_only':
      return 'Read-only operational visibility without mutation rights.';
    case 'user':
      return 'Legacy inference-only key without dashboard access.';
    default:
      return 'Workspace-scoped access.';
  }
}

/* ------------------------------------------------------------------ */
/*  Instance status labels                                             */
/* ------------------------------------------------------------------ */

export function instanceStatusClass(status: string): string {
  switch (status) {
    case 'running': return '';
    case 'error': return 'error';
    case 'starting': case 'stopping': case 'pending': case 'provisioning': return 'warning';
    case 'stopped': case 'terminating': case 'terminated': return 'inactive';
    default: return '';
  }
}

export function instanceStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    pending: 'Pending',
    provisioning: 'Provisioning',
    running: 'Running',
    starting: 'Starting',
    stopping: 'Stopping',
    stopped: 'Stopped',
    terminating: 'Terminating',
    terminated: 'Terminated',
    error: 'Error',
  };
  return labels[status] || 'Unknown';
}

/* ------------------------------------------------------------------ */
/*  Provider labels                                                    */
/* ------------------------------------------------------------------ */

export function capabilityLabels(capabilities?: ProviderCapabilities): string[] {
  if (!capabilities) return [];
  const labels: string[] = [];
  if (capabilities.supports_spot) labels.push('SPOT');
  if (capabilities.supports_start_stop) labels.push('START/STOP');
  if (capabilities.supports_custom_images) labels.push('CUSTOM IMAGES');
  if (capabilities.supports_region_selection) labels.push('REGIONS');
  if (capabilities.supports_public_ip) labels.push('PUBLIC IP');
  if (capabilities.supports_ssh_keys) labels.push('SSH KEYS');
  return labels;
}

export function providerLiveState(
  status?: ProviderStatus,
  configured?: boolean,
): { label: string; tone: string; detail: string } {
  if (!configured) {
    return { label: 'NOT CONFIGURED', tone: 'inactive', detail: 'No workspace-specific provider credential has been saved yet.' };
  }
  if (!status) {
    return { label: 'UNAVAILABLE', tone: 'warning', detail: 'Configuration exists, but the provider is not returning live status for this workspace.' };
  }
  if (status.connected) {
    return { label: 'CONNECTED', tone: '', detail: 'Provider is reachable and can return live account status.' };
  }
  if (status.error_code === 'auth_failed') {
    return { label: 'AUTH FAILED', tone: 'error', detail: 'Saved credentials were rejected by the provider.' };
  }
  if (status.error_code === 'rate_limited') {
    return { label: 'RATE LIMITED', tone: 'warning', detail: 'Provider is reachable but temporarily rate limiting status or offering requests.' };
  }
  return { label: 'DEGRADED', tone: 'warning', detail: status.error || 'Provider is configured but currently unreachable or unhealthy.' };
}

export function providerStateBadge(
  status?: ProviderStatus,
  configured?: boolean,
): { label: string; tone: string } {
  if (status?.provider === 'mock') {
    return status.connected
      ? { label: 'LOCAL READY', tone: '' }
      : { label: 'LOCAL OFFLINE', tone: 'warning' };
  }
  if (!configured) return { label: 'NOT CONFIGURED', tone: 'inactive' };
  if (!status) return { label: 'UNAVAILABLE', tone: 'warning' };
  if (status.connected) return { label: 'CONNECTED', tone: '' };
  if (status.error_code === 'auth_failed') return { label: 'AUTH FAILED', tone: 'error' };
  return { label: 'DEGRADED', tone: 'warning' };
}

/* ------------------------------------------------------------------ */
/*  GPU / offering labels                                              */
/* ------------------------------------------------------------------ */

export function formatGPUDisplayName(gpuType: GPUType, displayName?: string): string {
  if (displayName?.trim()) return displayName.trim();
  return gpuType.replace(/_/g, ' ');
}

export function formatOfferingRegion(region?: string, provider?: ProviderType): string {
  if (provider === 'mock' && (!region || region === 'mock')) return 'local lab';
  return region || 'global';
}

/* ------------------------------------------------------------------ */
/*  Deployment timeline labels                                         */
/* ------------------------------------------------------------------ */

export function timelineTone(state: DeploymentTimelineStep['state']): string {
  switch (state) {
    case 'done': return '';
    case 'active': return 'warning';
    case 'failed': return 'error';
    case 'stopped': case 'terminated': return 'inactive';
    default: return 'inactive';
  }
}

export function timelineLabel(state: DeploymentTimelineStep['state']): string {
  if (state === 'stopped') return 'STOPPED';
  if (state === 'terminated') return 'TERMINATED';
  return state.toUpperCase();
}

/* ------------------------------------------------------------------ */
/*  Verification / freshness labels                                    */
/* ------------------------------------------------------------------ */

export function verificationToneClass(freshness: 'fresh' | 'stale' | 'never' | 'recent'): string {
  if (freshness === 'stale') return 'status-warning';
  if (freshness === 'never') return 'status-inactive';
  return '';
}

/* ------------------------------------------------------------------ */
/*  Agent labels                                                       */
/* ------------------------------------------------------------------ */

export function formatAgentStatus(status?: string): string {
  return status ? status.replace(/_/g, ' ').toUpperCase() : 'IDLE';
}

export function formatStepType(type: string): string {
  return type.replace(/_/g, ' ').toUpperCase();
}

export function formatStepPayload(payload: unknown): string {
  if (typeof payload === 'string') return payload;
  if (payload == null) return '';
  try {
    return JSON.stringify(payload, null, 2);
  } catch {
    return String(payload);
  }
}
