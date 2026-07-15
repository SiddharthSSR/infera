export const generatedTypes = `export type ProviderType = 'e2e' | 'runpod' | 'vastai' | 'lambda' | 'mock';
export type KnownGPUType = 'RTX_4090' | 'RTX_4080' | 'A100_40GB' | 'A100_80GB' | 'H100' | 'L40S';
export type GPUType = KnownGPUType | (string & {});
export type InstanceStatus =
  | 'pending'
  | 'provisioning'
  | 'running'
  | 'stopping'
  | 'stopped'
  | 'terminating'
  | 'terminated'
  | 'error';
export type InstanceEngine = 'vllm' | 'sglang' | 'tensorrt_llm' | 'mock' | (string & {});

export interface Instance {
  id: string;
  provider_id: string;
  provider: ProviderType;
  workspace_id?: string;
  name: string;
  status: InstanceStatus;
  gpu_type: GPUType;
  gpu_count: number;
  vcpu: number;
  memory_gb: number;
  storage_gb: number;
  public_ip?: string;
  http_port?: number;
  ssh_port?: number;
  worker_id?: string;
  models?: string[];
  engine?: InstanceEngine;
  cost_per_hour: number;
  spot_instance: boolean;
  created_at: string;
  started_at?: string;
  stopped_at?: string;
  error?: string;
}

export interface InstancesResponse {
  instances: Instance[];
  total: number;
}

export interface GPUOffering {
  provider: ProviderType;
  gpu_type: GPUType;
  display_name?: string;
  provider_gpu_type_id?: string;
  gpu_count: number;
  vcpu: number;
  memory_gb: number;
  storage_gb: number;
  cost_per_hour: number;
  spot_price?: number;
  region: string;
  available: number;
}

export interface OfferingsResponse {
  offerings: GPUOffering[];
  total: number;
}

export interface ProviderCapabilities {
  supports_spot: boolean;
  supports_custom_images: boolean;
  supports_region_selection: boolean;
  supports_public_ip: boolean;
  supports_ssh_keys: boolean;
  supports_start_stop: boolean;
  startup_script_limit?: number;
  known_regions?: string[];
}

export interface ProviderStatus {
  provider: ProviderType;
  connected: boolean;
  account_id?: string;
  balance?: number;
  active_instances: number;
  quota_limit?: number;
  error?: string;
  error_code?: string;
  capabilities?: ProviderCapabilities;
}

export interface ProvidersResponse {
  providers: ProviderStatus[];
}

export interface CostSummary {
  current_hourly: number;
  today_total: number;
  month_total: number;
  projected_month: number;
  by_provider: Record<string, number>;
  by_gpu: Record<string, number>;
}
`;

export const fixtures = {
  'instances_list_response.json': {
    instances: [
      {
        id: 'inst_fixture_1',
        provider_id: 'runpod-fixture-1',
        provider: 'runpod',
        workspace_id: 'ws_alpha',
        name: 'Fixture Worker',
        status: 'running',
        gpu_type: 'H100',
        gpu_count: 1,
        vcpu: 32,
        memory_gb: 80,
        storage_gb: 500,
        public_ip: '203.0.113.10',
        http_port: 8081,
        ssh_port: 22,
        worker_id: 'worker-fixture-1',
        models: ['Qwen/Qwen2.5-7B-Instruct'],
        engine: 'sglang',
        cost_per_hour: 3.5,
        spot_instance: false,
        created_at: '2026-04-10T00:00:00Z',
        started_at: '2026-04-10T00:05:00Z',
        error: '',
      },
    ],
    total: 1,
  },
  'offerings_list_response.json': {
    offerings: [
      {
        provider: 'runpod',
        gpu_type: 'H100',
        display_name: 'NVIDIA H100 SXM',
        provider_gpu_type_id: 'h100-sxm',
        gpu_count: 1,
        vcpu: 32,
        memory_gb: 80,
        storage_gb: 500,
        cost_per_hour: 3.5,
        spot_price: 2.75,
        region: 'us-east-1',
        available: 3,
      },
    ],
    total: 1,
  },
  'providers_list_response.json': {
    providers: [
      {
        provider: 'runpod',
        connected: true,
        account_id: 'acct_fixture',
        balance: 42.5,
        active_instances: 1,
        quota_limit: 8,
        error: '',
        error_code: '',
        capabilities: {
          supports_spot: true,
          supports_custom_images: true,
          supports_region_selection: true,
          supports_public_ip: true,
          supports_ssh_keys: false,
          supports_start_stop: true,
          startup_script_limit: 16384,
          known_regions: ['us-east-1'],
        },
      },
    ],
  },
  'cost_summary_response.json': {
    current_hourly: 0,
    today_total: 0,
    month_total: 0,
    projected_month: 0,
    by_provider: {},
    by_gpu: {},
  },
};
