import type { 
  Worker, Model, Stats, ChatCompletionRequest, ChatCompletionResponse,
  Instance, GPUOffering, ProviderStatus, CostSummary, ProvisionRequest 
} from '../types';

const API_BASE = '';

export async function fetchWorkers(): Promise<Worker[]> {
  const response = await fetch(`${API_BASE}/api/workers`);
  if (!response.ok) throw new Error('Failed to fetch workers');
  const data = await response.json();
  return data.workers;
}

export async function fetchModels(): Promise<Model[]> {
  const response = await fetch(`${API_BASE}/v1/models`);
  if (!response.ok) throw new Error('Failed to fetch models');
  const data = await response.json();
  return data.data;
}

export async function fetchStats(): Promise<Stats> {
  const response = await fetch(`${API_BASE}/api/stats`);
  if (!response.ok) throw new Error('Failed to fetch stats');
  return response.json();
}

export async function sendChatCompletion(request: ChatCompletionRequest): Promise<ChatCompletionResponse> {
  const response = await fetch(`${API_BASE}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Request failed');
  }
  
  return response.json();
}

export async function* streamChatCompletion(
  request: ChatCompletionRequest
): AsyncGenerator<string, void, unknown> {
  const response = await fetch(`${API_BASE}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ ...request, stream: true }),
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Request failed');
  }

  const reader = response.body?.getReader();
  if (!reader) throw new Error('No response body');

  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const data = line.slice(6);
        if (data === '[DONE]') return;
        
        try {
          const parsed = JSON.parse(data);
          const content = parsed.choices?.[0]?.delta?.content;
          if (content) yield content;
        } catch {
          // Ignore parse errors
        }
      }
    }
  }
}

// Instance Management API

export async function fetchInstances(): Promise<Instance[]> {
  const response = await fetch(`${API_BASE}/api/instances`);
  if (!response.ok) throw new Error('Failed to fetch instances');
  const data = await response.json();
  return data.instances;
}

export async function fetchOfferings(): Promise<GPUOffering[]> {
  const response = await fetch(`${API_BASE}/api/offerings`);
  if (!response.ok) throw new Error('Failed to fetch offerings');
  const data = await response.json();
  return data.offerings;
}

export async function fetchProviders(): Promise<ProviderStatus[]> {
  const response = await fetch(`${API_BASE}/api/providers`);
  if (!response.ok) throw new Error('Failed to fetch providers');
  const data = await response.json();
  return data.providers;
}

export async function fetchCosts(): Promise<CostSummary> {
  const response = await fetch(`${API_BASE}/api/costs`);
  if (!response.ok) throw new Error('Failed to fetch costs');
  return response.json();
}

export async function provisionInstance(request: ProvisionRequest): Promise<Instance> {
  const response = await fetch(`${API_BASE}/api/instances/provision`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Provisioning failed');
  }
  
  const data = await response.json();
  return data.instance;
}

export async function terminateInstance(instanceId: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/instances/${instanceId}`, {
    method: 'DELETE',
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Termination failed');
  }
}

export async function startInstance(instanceId: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/instances/${instanceId}/start`, {
    method: 'POST',
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Start failed');
  }
}

export async function stopInstance(instanceId: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/instances/${instanceId}/stop`, {
    method: 'POST',
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Stop failed');
  }
}
