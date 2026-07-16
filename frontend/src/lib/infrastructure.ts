import type {
  CostSummary,
  GPUOffering,
  Instance,
  InstanceEngine,
  InstancesResponse,
  OfferingsResponse,
  ProviderCapabilities,
  ProviderStatus,
  ProvidersResponse,
} from '../types';

type JSONRecord = Record<string, unknown>;

function expectRecord(value: unknown, label: string): JSONRecord {
  if (value == null || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`Invalid ${label}`);
  }
  return value as JSONRecord;
}

function expectString(record: JSONRecord, key: string, label: string): string {
  const value = record[key];
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalString(record: JSONRecord, key: string, label: string): string | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function expectNumber(record: JSONRecord, key: string, label: string): number {
  const value = record[key];
  if (typeof value !== 'number' || Number.isNaN(value)) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalNumber(record: JSONRecord, key: string, label: string): number | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  if (typeof value !== 'number' || Number.isNaN(value)) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function expectBoolean(record: JSONRecord, key: string, label: string): boolean {
  const value = record[key];
  if (typeof value !== 'boolean') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalStringArray(record: JSONRecord, key: string, label: string): string[] | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  if (!Array.isArray(value) || value.some((item) => typeof item !== 'string')) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value as string[];
}

function expectNumberRecord(record: JSONRecord, key: string, label: string): Record<string, number> {
  const value = expectRecord(record[key], `${label}.${key}`);
  const parsed: Record<string, number> = {};
  for (const [entryKey, entryValue] of Object.entries(value)) {
    if (typeof entryValue !== 'number' || Number.isNaN(entryValue)) {
      throw new Error(`Invalid ${label}.${key}.${entryKey}`);
    }
    parsed[entryKey] = entryValue;
  }
  return parsed;
}

function parseInstance(value: unknown, label: string): Instance {
  const record = expectRecord(value, label);
  return {
    id: expectString(record, 'id', label),
    provider_id: expectString(record, 'provider_id', label),
    provider: expectString(record, 'provider', label) as Instance['provider'],
    workspace_id: optionalString(record, 'workspace_id', label),
    name: expectString(record, 'name', label),
    status: expectString(record, 'status', label) as Instance['status'],
    gpu_type: expectString(record, 'gpu_type', label) as Instance['gpu_type'],
    gpu_count: expectNumber(record, 'gpu_count', label),
    vcpu: expectNumber(record, 'vcpu', label),
    memory_gb: expectNumber(record, 'memory_gb', label),
    storage_gb: expectNumber(record, 'storage_gb', label),
    public_ip: optionalString(record, 'public_ip', label),
    http_port: optionalNumber(record, 'http_port', label),
    ssh_port: optionalNumber(record, 'ssh_port', label),
    worker_id: optionalString(record, 'worker_id', label),
    models: optionalStringArray(record, 'models', label),
    engine: optionalString(record, 'engine', label) as InstanceEngine | undefined,
    cost_per_hour: expectNumber(record, 'cost_per_hour', label),
    spot_instance: expectBoolean(record, 'spot_instance', label),
    created_at: expectString(record, 'created_at', label),
    started_at: optionalString(record, 'started_at', label),
    stopped_at: optionalString(record, 'stopped_at', label),
    error: optionalString(record, 'error', label),
  };
}

function parseOffering(value: unknown, label: string): GPUOffering {
  const record = expectRecord(value, label);
  return {
    provider: expectString(record, 'provider', label) as GPUOffering['provider'],
    gpu_type: expectString(record, 'gpu_type', label) as GPUOffering['gpu_type'],
    display_name: optionalString(record, 'display_name', label),
    provider_gpu_type_id: optionalString(record, 'provider_gpu_type_id', label),
    gpu_count: expectNumber(record, 'gpu_count', label),
    vcpu: expectNumber(record, 'vcpu', label),
    memory_gb: expectNumber(record, 'memory_gb', label),
    storage_gb: expectNumber(record, 'storage_gb', label),
    cost_per_hour: expectNumber(record, 'cost_per_hour', label),
    spot_price: optionalNumber(record, 'spot_price', label),
    region: expectString(record, 'region', label),
    available: expectNumber(record, 'available', label),
  };
}

function parseProviderCapabilities(value: unknown, label: string): ProviderCapabilities {
  const record = expectRecord(value, label);
  return {
    supports_spot: expectBoolean(record, 'supports_spot', label),
    supports_custom_images: expectBoolean(record, 'supports_custom_images', label),
    supports_region_selection: expectBoolean(record, 'supports_region_selection', label),
    supports_public_ip: expectBoolean(record, 'supports_public_ip', label),
    supports_ssh_keys: expectBoolean(record, 'supports_ssh_keys', label),
    supports_start_stop: expectBoolean(record, 'supports_start_stop', label),
    startup_script_limit: optionalNumber(record, 'startup_script_limit', label),
    known_regions: optionalStringArray(record, 'known_regions', label),
  };
}

function parseProviderStatus(value: unknown, label: string): ProviderStatus {
  const record = expectRecord(value, label);
  return {
    provider: expectString(record, 'provider', label) as ProviderStatus['provider'],
    connected: expectBoolean(record, 'connected', label),
    account_id: optionalString(record, 'account_id', label),
    balance: optionalNumber(record, 'balance', label),
    active_instances: expectNumber(record, 'active_instances', label),
    quota_limit: optionalNumber(record, 'quota_limit', label),
    error: optionalString(record, 'error', label),
    error_code: optionalString(record, 'error_code', label),
    capabilities:
      record.capabilities == null
        ? undefined
        : parseProviderCapabilities(record.capabilities, `${label}.capabilities`),
  };
}

export function parseInstancesResponse(value: unknown): InstancesResponse {
  const record = expectRecord(value, 'instances response');
  const items = record.instances;
  if (!Array.isArray(items)) {
    throw new Error('Invalid instances response.instances');
  }
  return {
    instances: items.map((item, index) => parseInstance(item, `instances response.instances[${index}]`)),
    total: expectNumber(record, 'total', 'instances response'),
  };
}

export function parseOfferingsResponse(value: unknown): OfferingsResponse {
  const record = expectRecord(value, 'offerings response');
  const items = record.offerings;
  if (!Array.isArray(items)) {
    throw new Error('Invalid offerings response.offerings');
  }
  return {
    offerings: items.map((item, index) => parseOffering(item, `offerings response.offerings[${index}]`)),
    total: expectNumber(record, 'total', 'offerings response'),
  };
}

export function parseProvidersResponse(value: unknown): ProvidersResponse {
  const record = expectRecord(value, 'providers response');
  const items = record.providers;
  if (!Array.isArray(items)) {
    throw new Error('Invalid providers response.providers');
  }
  return {
    providers: items.map((item, index) => parseProviderStatus(item, `providers response.providers[${index}]`)),
  };
}

export function parseCostSummary(value: unknown): CostSummary {
  const record = expectRecord(value, 'cost summary');
  return {
    current_hourly: expectNumber(record, 'current_hourly', 'cost summary'),
    today_total: expectNumber(record, 'today_total', 'cost summary'),
    month_total: expectNumber(record, 'month_total', 'cost summary'),
    projected_month: expectNumber(record, 'projected_month', 'cost summary'),
    by_provider: expectNumberRecord(record, 'by_provider', 'cost summary'),
    by_gpu: expectNumberRecord(record, 'by_gpu', 'cost summary'),
  };
}
