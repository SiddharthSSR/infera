import type {
  AgentAnalysisDepth,
  AgentAttachment,
  AgentDescriptor,
  AgentExecutionMode,
  AgentRun,
  AgentRunDetail,
} from '../types';
import { API_BASE, authFetch, readResponseError } from './apiCore';

export interface AgentsListResponse {
  agents: AgentDescriptor[];
  default_agent_id: string;
}

export interface CreateAgentRunRequest {
  agent_id?: string;
  mode?: AgentExecutionMode;
  analysis_depth?: AgentAnalysisDepth;
  model: string;
  input: string;
  max_steps?: number;
  attachments?: string[];
}

export async function fetchAgents(): Promise<AgentsListResponse> {
  const response = await authFetch(`${API_BASE}/api/agents`);
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to fetch agents'));
  }
  return response.json();
}

export async function uploadAgentAttachment(file: File): Promise<AgentAttachment> {
  const form = new FormData();
  form.append('file', file);

  const response = await authFetch(`${API_BASE}/api/agent-attachments`, {
    method: 'POST',
    body: form,
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to upload screenshot'));
  }
  const data = await response.json();
  return data.attachment;
}

export async function createAgentRun(request: CreateAgentRunRequest): Promise<AgentRun> {
  const response = await authFetch(`${API_BASE}/api/agents/runs`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to start agent run'));
  }

  const data = await response.json();
  return data.run;
}

export async function fetchAgentRunDetail(runID: string): Promise<AgentRunDetail> {
  const response = await authFetch(`${API_BASE}/api/agents/runs/${runID}`);
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to fetch agent run'));
  }

  const data = await response.json();
  return {
    run: data.run,
    steps: data.steps || [],
    attachments: data.attachments || [],
    sources: data.sources || [],
  };
}

export async function cancelAgentRun(runID: string): Promise<AgentRun> {
  const response = await authFetch(`${API_BASE}/api/agents/runs/${runID}/cancel`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to cancel agent run'));
  }

  const data = await response.json();
  return data.run;
}
