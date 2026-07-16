import type {
  DeploymentAttemptRecord,
  DeploymentAttemptResponse,
  DeploymentAttemptsResponse,
  DeploymentInferenceVerification,
  DeploymentProvisionRequest,
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

function optionalBoolean(record: JSONRecord, key: string, label: string): boolean | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
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

function optionalStringRecord(record: JSONRecord, key: string, label: string): Record<string, string> | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  const parsed = expectRecord(value, `${label}.${key}`);
  const out: Record<string, string> = {};
  for (const [entryKey, entryValue] of Object.entries(parsed)) {
    if (typeof entryValue !== 'string') {
      throw new Error(`Invalid ${label}.${key}.${entryKey}`);
    }
    out[entryKey] = entryValue;
  }
  return out;
}

function parseProvisionRequest(value: unknown, label: string): DeploymentProvisionRequest {
  const record = expectRecord(value, label);
  return {
    name: optionalString(record, 'name', label),
    provider: optionalString(record, 'provider', label) as DeploymentProvisionRequest['provider'],
    workspace_id: optionalString(record, 'workspace_id', label),
    gpu_type: expectString(record, 'gpu_type', label) as DeploymentProvisionRequest['gpu_type'],
    provider_gpu_type_id: optionalString(record, 'provider_gpu_type_id', label),
    gpu_count: optionalNumber(record, 'gpu_count', label),
    allowed_cuda_versions: optionalStringArray(record, 'allowed_cuda_versions', label),
    options: optionalStringRecord(record, 'options', label),
    region: optionalString(record, 'region', label),
    spot_instance: optionalBoolean(record, 'spot_instance', label),
    max_cost_hour: optionalNumber(record, 'max_cost_hour', label),
    models: optionalStringArray(record, 'models', label),
    engine: optionalString(record, 'engine', label) as DeploymentProvisionRequest['engine'],
    gateway_address: optionalString(record, 'gateway_address', label),
    docker_image: optionalString(record, 'docker_image', label),
    ssh_public_key: optionalString(record, 'ssh_public_key', label),
  };
}

function parseInferenceVerification(value: unknown, label: string): DeploymentInferenceVerification {
  const record = expectRecord(value, label);
  return {
    status: expectString(record, 'status', label) as DeploymentInferenceVerification['status'],
    verified_at: expectString(record, 'verified_at', label),
    latency_ms: optionalNumber(record, 'latency_ms', label),
    model: optionalString(record, 'model', label),
    response_preview: optionalString(record, 'response_preview', label),
    error: optionalString(record, 'error', label),
  };
}

export function parseDeploymentAttemptRecord(value: unknown, label = 'deployment attempt'): DeploymentAttemptRecord {
  const record = expectRecord(value, label);
  return {
    id: expectString(record, 'id', label),
    workspace_id: optionalString(record, 'workspace_id', label),
    created_by_key_id: optionalString(record, 'created_by_key_id', label),
    created_at: expectString(record, 'created_at', label),
    updated_at: expectString(record, 'updated_at', label),
    outcome: expectString(record, 'outcome', label) as DeploymentAttemptRecord['outcome'],
    request: parseProvisionRequest(record.request, `${label}.request`),
    selected_model_name: optionalString(record, 'selected_model_name', label),
    instance_id: optionalString(record, 'instance_id', label),
    instance_name: optionalString(record, 'instance_name', label),
    failure_reason: optionalString(record, 'failure_reason', label),
    auto_verification_requested_at: optionalString(record, 'auto_verification_requested_at', label),
    inference_verification:
      record.inference_verification == null
        ? undefined
        : parseInferenceVerification(record.inference_verification, `${label}.inference_verification`),
  };
}

export function parseDeploymentAttemptsResponse(value: unknown): DeploymentAttemptsResponse {
  const record = expectRecord(value, 'deployment attempts response');
  const items = record.attempts;
  if (!Array.isArray(items)) {
    throw new Error('Invalid deployment attempts response.attempts');
  }
  return {
    attempts: items.map((item, index) => parseDeploymentAttemptRecord(item, `deployment attempts response.attempts[${index}]`)),
    total: expectNumber(record, 'total', 'deployment attempts response'),
  };
}

export function parseDeploymentAttemptResponse(value: unknown): DeploymentAttemptResponse {
  const record = expectRecord(value, 'deployment attempt response');
  return {
    attempt: parseDeploymentAttemptRecord(record.attempt, 'deployment attempt response.attempt'),
  };
}
