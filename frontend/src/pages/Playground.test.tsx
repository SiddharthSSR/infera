/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React, { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Playground } from './Playground';
import { ChatContext, type ChatContextType, type Message, type PlaygroundHistoryEntry } from '../lib/chat-context';
import type { AgentAnalysisDepth, AgentExecutionMode, AgentRunDetail, PlaygroundMode } from '../types';

const hookMocks = vi.hoisted(() => ({
  useModels: vi.fn(),
  useAgents: vi.fn(),
}));

const apiMocks = vi.hoisted(() => ({
  streamChatCompletion: vi.fn(),
  createAgentRun: vi.fn(),
  fetchAgentRunDetail: vi.fn(),
  cancelAgentRun: vi.fn(),
  uploadAgentAttachment: vi.fn(),
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
  uploadAgentAttachment: apiMocks.uploadAgentAttachment,
}));

function PlaygroundProvider({ children }: { children: React.ReactNode }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [history, setHistory] = useState<PlaygroundHistoryEntry[]>([]);
  const [playgroundMode, setPlaygroundMode] = useState<PlaygroundMode>('chat');
  const [selectedAgentID, setSelectedAgentID] = useState('');
  const [agentMaxSteps, setAgentMaxSteps] = useState(8);
  const [agentExecutionMode, setAgentExecutionMode] = useState<AgentExecutionMode>('operations');
  const [agentAnalysisDepth, setAgentAnalysisDepth] = useState<AgentAnalysisDepth>('standard');
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
    agentExecutionMode,
    setAgentExecutionMode,
    agentAnalysisDepth,
    setAgentAnalysisDepth,
    selectedModel,
    setSelectedModel,
    temperature,
    setTemperature,
    maxTokens,
    setMaxTokens,
  };

  return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>;
}

function successDetail(): AgentRunDetail {
  return {
    run: {
      id: 'run_1',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      mode: 'research',
      analysis_depth: 'deep',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Investigate provider outage',
      status: 'succeeded',
      max_steps: 12,
      current_step: 2,
      final_output: 'Provider connectivity is healthy.\n\n- RunPod status is normal.\n- Gateway pressure is low.\n- No quota risk detected.',
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:02Z',
    },
    steps: [
      {
        id: 1,
        run_id: 'run_1',
        index: 0,
        type: 'tool_call',
        tool_name: 'web_search',
        payload: { arguments: { query: 'RunPod status', max_results: 3 } },
        created_at: '2026-03-31T12:00:01Z',
      },
      {
        id: 2,
        run_id: 'run_1',
        index: 1,
        type: 'tool_result',
        tool_name: 'web_search',
        payload: {
          ok: true,
          result: {
            results: [
              {
                title: 'RunPod Status',
                url: 'https://status.runpod.io/',
                domain: 'status.runpod.io',
              },
            ],
          },
        },
        created_at: '2026-03-31T12:00:01Z',
      },
      {
        id: 3,
        run_id: 'run_1',
        index: 2,
        type: 'final',
        payload: { message: 'Provider connectivity is healthy.' },
        created_at: '2026-03-31T12:00:02Z',
      },
    ],
    attachments: [],
    sources: [
      {
        title: 'RunPod Status',
        url: 'https://status.runpod.io/',
        domain: 'status.runpod.io',
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
      mode: 'operations',
      analysis_depth: 'standard',
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
    attachments: [],
    sources: [],
  };
}

describe('Playground agent mode', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('URL', {
      createObjectURL: vi.fn(() => 'blob:preview'),
      revokeObjectURL: vi.fn(),
    });

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
              { name: 'list_workers', description: 'List worker health.', modes: ['operations'] },
              { name: 'get_gateway_stats', description: 'Read gateway stats.', modes: ['operations'] },
              { name: 'get_usage_summary', description: 'Read usage.', modes: ['operations'] },
              { name: 'get_quota_status', description: 'Read quota pressure.', modes: ['operations'] },
              { name: 'web_search', description: 'Search allowlisted official sources.', modes: ['research'] },
              { name: 'vision_analyze', description: 'Inspect uploaded screenshots.', modes: ['multimodal'] },
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

  it('shows a derived thinking state before rendering narrative output and sources', async () => {
    let resolveDetail: ((detail: AgentRunDetail) => void) | null = null;

    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_1',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      mode: 'research',
      analysis_depth: 'deep',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Investigate provider outage',
      status: 'queued',
      max_steps: 12,
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
    fireEvent.click(screen.getByRole('button', { name: 'RESEARCH' }));
    fireEvent.click(screen.getByRole('button', { name: 'DEEP' }));
    fireEvent.change(screen.getByPlaceholderText(/investigate and cite official sources/i), {
      target: { value: 'Investigate provider outage' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'RUN AGENT' }));

    await waitFor(() => {
      expect(screen.getByText('Planning workspace review')).toBeInTheDocument();
    });

    await act(async () => {
      resolveDetail?.(successDetail());
    });

    await waitFor(() => {
      expect(screen.getByText(/Provider connectivity is healthy/i)).toBeInTheDocument();
    });

    expect(apiMocks.createAgentRun).toHaveBeenCalledWith({
      agent_id: 'hermes',
      mode: 'research',
      analysis_depth: 'deep',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Investigate provider outage',
      max_steps: 8,
      attachments: [],
    });
    expect(screen.getByRole('link', { name: /status.runpod.io/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /run trace/i })).toHaveAttribute('aria-expanded', 'false');
  });

  it('auto-expands the run trace when an agent run fails', async () => {
    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_2',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      mode: 'operations',
      analysis_depth: 'standard',
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
    fireEvent.change(screen.getByPlaceholderText(/inspect workspace health/i), {
      target: { value: 'Inspect workspace health' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'RUN AGENT' }));

    await waitFor(() => {
      expect(screen.getByText(/Run failed/i)).toBeInTheDocument();
    });

    expect(screen.getByRole('button', { name: /run trace/i })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText(/invalid_json_action/i)).toBeInTheDocument();
  });

  it('uploads a screenshot before starting a multimodal run', async () => {
    apiMocks.uploadAgentAttachment.mockResolvedValue({
      id: 'att_1',
      workspace_id: 'ws_alpha',
      file_name: 'console.png',
      mime_type: 'image/png',
      size_bytes: 1024,
      sha256: 'abc',
      created_at: '2026-03-31T12:00:00Z',
    });
    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_3',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      mode: 'multimodal',
      analysis_depth: 'standard',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'What does this screenshot imply?',
      status: 'queued',
      max_steps: 8,
      current_step: 0,
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:00Z',
    });
    apiMocks.fetchAgentRunDetail.mockResolvedValueOnce({
      ...successDetail(),
      run: {
        ...successDetail().run,
        id: 'run_3',
        mode: 'multimodal',
        analysis_depth: 'standard',
      },
      attachments: [
        {
          id: 'att_1',
          workspace_id: 'ws_alpha',
          run_id: 'run_3',
          file_name: 'console.png',
          mime_type: 'image/png',
          size_bytes: 1024,
          sha256: 'abc',
          created_at: '2026-03-31T12:00:00Z',
        },
      ],
    });

    render(
      <PlaygroundProvider>
        <Playground />
      </PlaygroundProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'AGENT' }));
    fireEvent.click(screen.getByRole('button', { name: 'MULTIMODAL' }));
    fireEvent.change(screen.getByPlaceholderText(/what this screenshot shows/i), {
      target: { value: 'What does this screenshot imply?' },
    });

    const input = screen.getByLabelText(/screenshot upload/i) as HTMLInputElement;
    const file = new File(['test'], 'console.png', { type: 'image/png' });
    await act(async () => {
      fireEvent.change(input, { target: { files: [file] } });
    });

    fireEvent.click(screen.getByRole('button', { name: 'RUN AGENT' }));

    await waitFor(() => {
      expect(apiMocks.uploadAgentAttachment).toHaveBeenCalledWith(file);
    });
    await waitFor(() => {
      expect(apiMocks.createAgentRun).toHaveBeenCalledWith(expect.objectContaining({
        mode: 'multimodal',
        attachments: ['att_1'],
      }));
    });
  });

  it('filters the visible tool list by selected agent mode', () => {
    render(
      <PlaygroundProvider>
        <Playground />
      </PlaygroundProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'AGENT' }));

    expect(screen.getByText('list_workers')).toBeInTheDocument();
    expect(screen.queryByText('web_search')).not.toBeInTheDocument();
    expect(screen.queryByText('vision_analyze')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'RESEARCH' }));
    expect(screen.getByText('web_search')).toBeInTheDocument();
    expect(screen.queryByText('list_workers')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'MULTIMODAL' }));
    expect(screen.getByText('vision_analyze')).toBeInTheDocument();
    expect(screen.queryByText('web_search')).not.toBeInTheDocument();
  });
});
