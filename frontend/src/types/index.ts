// API Types

export interface Worker {
  worker_id: string;
  address: string;
  status: 'healthy' | 'degraded' | 'unhealthy' | 'draining' | 'offline';
  models: string[];
  gpu_utilization: number;
  memory_used: number;
  memory_total: number;
  queue_depth: number;
  requests_per_sec: number;
  avg_latency_ms: number;
  p50_latency_ms: number;
  p99_latency_ms: number;
  error_rate: number;
  last_heartbeat: string;
}

export interface Model {
  id: string;
  object: string;
  created: number;
  owned_by: string;
  // Vault fields (present when vault is connected)
  loaded?: boolean;
  family?: string;
  parameters?: string;
  quantization?: string;
  vram_required?: number;
  max_context?: number;
  tags?: string[];
  vault_status?: string;
}

export interface Stats {
  workers: {
    total: number;
    healthy: number;
  };
  models: {
    available: number;
  };
  requests: {
    per_second: number;
    queue_depth: number;
  };
  latency: {
    avg_ms: number;
  };
  gpu: {
    avg_utilization: number;
  };
  memory: {
    used_bytes: number;
    total_bytes: number;
  };
  uptime_seconds: number;
}

export interface ChatMessage {
  role: 'system' | 'user' | 'assistant';
  content: string;
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  temperature?: number;
  max_tokens?: number;
  stream?: boolean;
}

export interface ChatCompletionResponse {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: {
    index: number;
    message: ChatMessage;
    finish_reason: string;
  }[];
  usage: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
}

// GPU Provider Types

export type ProviderType = 'e2e' | 'runpod' | 'vastai' | 'lambda' | 'mock';
export type KnownGPUType = 'RTX_4090' | 'RTX_4080' | 'A100_40GB' | 'A100_80GB' | 'H100' | 'L40S';
export type GPUType = KnownGPUType | (string & {});
export type InstanceStatus = 'pending' | 'provisioning' | 'running' | 'stopping' | 'stopped' | 'terminating' | 'terminated' | 'error';

export interface Instance {
  id: string;
  provider_id: string;
  provider: ProviderType;
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
  cost_per_hour: number;
  spot_instance: boolean;
  created_at: string;
  started_at?: string;
  stopped_at?: string;
  error?: string;
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

export interface CostSummary {
  current_hourly: number;
  today_total: number;
  month_total: number;
  projected_month: number;
  by_provider: Record<string, number>;
  by_gpu: Record<string, number>;
}

export interface ProvisionRequest {
  name?: string;
  provider?: ProviderType;
  gpu_type: GPUType;
  provider_gpu_type_id?: string;
  gpu_count?: number;
  region?: string;
  spot_instance?: boolean;
  max_cost_hour?: number;
  models?: string[];
  selected_model_name?: string;
}

// Vault (Model Registry) Types

export interface VaultModel {
  id: string;
  name: string;
  source: string;
  source_uri: string;
  parameters: string;
  quantization: string;
  vram_required: number;
  max_context: number;
  family: string;
  tags: string[];
  metadata: Record<string, string>;
  status: 'available' | 'testing' | 'deprecated';
  created_at: string;
  updated_at: string;
}

export interface VaultStats {
  total_models: number;
  available_models: number;
  deprecated_models: number;
  model_families: number;
}

export interface VaultModelFilter {
  family?: string;
  status?: string;
  search?: string;
  quantization?: string;
  tag?: string;
  min_vram?: number;
  max_vram?: number;
}

export interface CreateVaultModelInput {
  name: string;
  source_uri: string;
  source?: string;
  parameters?: string;
  quantization?: string;
  vram_required?: number;
  max_context?: number;
  family?: string;
  tags?: string[];
  metadata?: Record<string, string>;
}
