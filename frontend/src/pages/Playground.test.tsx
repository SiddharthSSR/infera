/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React, { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Playground } from './Playground';
import { ChatContext, type ChatContextType, type Message, type PlaygroundHistoryEntry } from '../lib/chat-context';
import type { AgentRunDetail, PlaygroundMode } from '../types';

const hookMocks = vi.hoisted(() => ({
  useModels: vi.fn(),
  useAgents: vi.fn(),
}));

const apiMocks = vi.hoisted(() => ({
  streamChatCompletion: vi.fn(),
  createAgentRun: vi.fn(),
  fetchAgentRunDetail: vi.fn(),
  cancelAgentRun: vi.fn(),
}));

vi.mock('../hooks/useApi', () => ({
  useModels: hookMocks.useModels,
  useAgents: hookMocks.useAgents,
}));

vi.mock('../lib/api', () => ({
  streamChatCompletion: apiMocks.streamChatCompletion,
  createAgentRun: apiMocks.createAgentRun,
  fetchAgentRunDetail: apiMocks.fetchAgentRunDetail,
  cancelAgentRun: apiMocks.cancelAgentRun,
}));

function PlaygroundProvider({ children }: { children: React.ReactNode }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [history, setHistory] = useState<PlaygroundHistoryEntry[]>([]);
  const [playgroundMode, setPlaygroundMode] = useState<PlaygroundMode>('chat');
  const [selectedAgentID, setSelectedAgentID] = useState('');
  const [agentMaxSteps, setAgentMaxSteps] = useState(8);
  const [selectedModel, setSelectedModel] = useState('');
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(2048);

  const value: ChatContextType = {
    messages,
    setMessages,
    history,
    setHistory,
    playgroundMode,
    setPlaygroundMode,
    selectedAgentID,
    setSelectedAgentID,
    agentMaxSteps,
    setAgentMaxSteps,
    selectedModel,
    setSelectedModel,
    temperature,
    setTemperature,
    maxTokens,
    setMaxTokens,
  };

  return (
    <ChatContext.Provider value={value}>
      {children}
    </ChatContext.Provider>
  );
}

function successDetail(): AgentRunDetail {
  return {
    run: {
      id: 'run_1',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workspace health',
      status: 'succeeded',
      max_steps: 8,
      current_step: 2,
      final_output: 'Workspace is healthy.\n\n- Workers are stable.\n- Gateway pressure is low.\n- Quota headroom looks healthy.',
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:02Z',
    },
    steps: [
      {
        id: 1,
        run_id: 'run_1',
        index: 0,
        type: 'tool_call',
        tool_name: 'list_workers',
        payload: { arguments: {} },
        created_at: '2026-03-31T12:00:01Z',
      },
      {
        id: 2,
        run_id: 'run_1',
        index: 1,
        type: 'tool_result',
        tool_name: 'list_workers',
        payload: { ok: true, result: [{ worker_id: 'w1', status: 'healthy' }] },
        created_at: '2026-03-31T12:00:01Z',
      },
      {
        id: 3,
        run_id: 'run_1',
        index: 2,
        type: 'final',
        payload: { message: 'Workspace is healthy.' },
        created_at: '2026-03-31T12:00:02Z',
      },
    ],
  };
}

function failedDetail(): AgentRunDetail {
  return {
    run: {
      id: 'run_2',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workspace health',
      status: 'failed',
      max_steps: 8,
      current_step: 1,
      failure_reason: 'invalid JSON action from model',
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:02Z',
    },
    steps: [
      {
        id: 1,
        run_id: 'run_2',
        index: 0,
        type: 'error',
        payload: { error: 'invalid_json_action', message: 'invalid JSON action from model' },
        created_at: '2026-03-31T12:00:01Z',
      },
    ],
  };
}

describe('Playground agent mode', () => {
  beforeEach(() => {
    vi.clearAllMocks();

    hookMocks.useModels.mockReturnValue({
      data: [
        {
          id: 'Qwen/Qwen2.5-7B-Instruct',
          object: 'model',
          created: 0,
          owned_by: 'infera',
          loaded: true,
        },
      ],
    });

    hookMocks.useAgents.mockReturnValue({
      data: {
        default_agent_id: 'hermes',
        agents: [
          {
            id: 'hermes',
            name: 'Hermes',
            description: 'Read-only workspace health copilot.',
            default_max_steps: 8,
            tools: [
              { name: 'list_workers', description: 'List worker health.' },
              { name: 'get_gateway_stats', description: 'Read gateway stats.' },
              { name: 'get_usage_summary', description: 'Read usage.' },
              { name: 'get_quota_status', description: 'Read quota pressure.' },
            ],
          },
        ],
      },
      error: null,
    });

    apiMocks.streamChatCompletion.mockImplementation(async function* () {
      yield 'chat output';
    });
  });

  it('shows a derived thinking state before rendering the final narrative output', async () => {
    let resolveDetail: ((detail: AgentRunDetail) => void) | null = null;

    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_1',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workspace health',
      status: 'queued',
      max_steps: 8,
      current_step: 0,
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:00Z',
    });
    apiMocks.fetchAgentRunDetail.mockImplementationOnce(() => new Promise((resolve) => {
      resolveDetail = resolve as (detail: AgentRunDetail) => void;
    }));

    render(
      <PlaygroundProvider>
        <Playground />
      </PlaygroundProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'AGENT' }));
    fireEvent.change(screen.getByPlaceholderText(/Ask Hermes to inspect workspace health/i), {
      target: { value: 'Inspect workspace health' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'RUN AGENT' }));

    await waitFor(() => {
      expect(screen.getByText('Planning workspace review')).toBeInTheDocument();
    });

    await act(async () => {
      resolveDetail?.(successDetail());
    });

    await waitFor(() => {
      expect(screen.getByText(/Workspace is healthy/i)).toBeInTheDocument();
    });

    expect(apiMocks.createAgentRun).toHaveBeenCalledWith({
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workspace health',
      max_steps: 8,
    });
    expect(screen.getByRole('button', { name: /run trace/i })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('STEP 1')).not.toBeInTheDocument();
  });

  it('auto-expands the run trace when an agent run fails', async () => {
    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_2',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workspace health',
      status: 'queued',
      max_steps: 8,
      current_step: 0,
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:00Z',
    });
    apiMocks.fetchAgentRunDetail.mockResolvedValueOnce(failedDetail());

    render(
      <PlaygroundProvider>
        <Playground />
      </PlaygroundProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'AGENT' }));
    fireEvent.change(screen.getByPlaceholderText(/Ask Hermes to inspect workspace health/i), {
      target: { value: 'Inspect workspace health' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'RUN AGENT' }));

    await waitFor(() => {
      expect(screen.getByText(/Run failed: invalid JSON action from model/i)).toBeInTheDocument();
    });

    expect(screen.getByRole('button', { name: /run trace/i })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('STEP 1')).toBeInTheDocument();
  });
});
