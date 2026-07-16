export const generatedTypes = `import type { GPUType, InstanceEngine, ProviderType } from './infrastructure';

export type DeploymentAttemptOutcome = 'provisioned' | 'request_failed';

export interface DeploymentProvisionRequest {
  name?: string;
  provider?: ProviderType;
  workspace_id?: string;
  gpu_type: GPUType;
  provider_gpu_type_id?: string;
  gpu_count?: number;
  allowed_cuda_versions?: string[];
  options?: Record<string, string>;
  region?: string;
  spot_instance?: boolean;
  max_cost_hour?: number;
  models?: string[];
  engine?: InstanceEngine;
  gateway_address?: string;
  docker_image?: string;
  ssh_public_key?: string;
}

export interface DeploymentInferenceVerification {
  status: 'passed' | 'failed';
  verified_at: string;
  latency_ms?: number;
  model?: string;
  response_preview?: string;
  error?: string;
}

export interface DeploymentAttemptRecord {
  id: string;
  workspace_id?: string;
  created_by_key_id?: string;
  created_at: string;
  updated_at: string;
  outcome: DeploymentAttemptOutcome;
  request: DeploymentProvisionRequest;
  selected_model_name?: string;
  instance_id?: string;
  instance_name?: string;
  failure_reason?: string;
  auto_verification_requested_at?: string;
  inference_verification?: DeploymentInferenceVerification;
}

export interface DeploymentAttemptsResponse {
  attempts: DeploymentAttemptRecord[];
  total: number;
}

export interface DeploymentAttemptResponse {
  attempt: DeploymentAttemptRecord;
}

export interface DeploymentAutoVerificationRequest {
  requested_at: string;
}
`;

const provisionedAttempt = {
  id: 'attempt_fixture_1',
  workspace_id: 'ws_alpha',
  created_by_key_id: 'key_fixture',
  created_at: '2026-04-10T00:00:00Z',
  updated_at: '2026-04-10T00:01:00Z',
  outcome: 'provisioned',
  request: {
    name: 'fixture-worker',
    provider: 'mock',
    workspace_id: 'ws_alpha',
    gpu_type: 'RTX_4090',
    gpu_count: 1,
    spot_instance: false,
    models: ['org/model-a'],
    engine: 'sglang',
    options: {
      INFERA_SGLANG_CHUNKED_PREFILL_SIZE: '2048',
      INFERA_SGLANG_MAX_RUNNING_REQUESTS: '32',
      INFERA_SGLANG_MEM_FRACTION_STATIC: '0.90',
    },
  },
  selected_model_name: 'Model A',
  instance_id: 'inst_fixture_1',
  instance_name: 'fixture-worker',
  inference_verification: {
    status: 'passed',
    verified_at: '2026-04-10T00:01:00Z',
    latency_ms: 321,
    model: 'org/model-a',
    response_preview: 'READY',
  },
};

export const fixtures = {
  'deployment_attempt_auto_verification_request.json': {
    requested_at: '2026-04-10T00:00:30Z',
  },
  'deployment_attempt_auto_verification_response.json': {
    attempt: {
      ...provisionedAttempt,
      auto_verification_requested_at: '2026-04-10T00:00:30Z',
    },
  },
  'deployment_attempt_verification_request.json': {
    status: 'passed',
    verified_at: '2026-04-10T00:01:00Z',
    latency_ms: 321,
    model: 'org/model-a',
    response_preview: 'READY',
  },
  'deployment_attempt_verification_response.json': {
    attempt: provisionedAttempt,
  },
  'deployment_attempts_list_response.json': {
    attempts: [
      {
        ...provisionedAttempt,
        auto_verification_requested_at: '2026-04-10T00:00:30Z',
      },
    ],
    total: 1,
  },
};
