/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React, { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Playground } from './Playground';
import { ChatContext, type ChatContextType, type Message, type PlaygroundHistoryEntry } from '../lib/chat-context';
import type { PlaygroundMode } from '../types';

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
            description: 'Operational assistant for runtime visibility.',
            default_max_steps: 8,
            tools: [
              { name: 'list_workers', description: 'List worker health.' },
              { name: 'get_gateway_stats', description: 'Read gateway stats.' },
            ],
          },
        ],
      },
      error: null,
    });

    apiMocks.streamChatCompletion.mockImplementation(async function* () {
      yield 'chat output';
    });

    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_1',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workers',
      status: 'queued',
      max_steps: 8,
      current_step: 0,
      created_at: '2026-03-31T12:00:00Z',
      updated_at: '2026-03-31T12:00:00Z',
    });

    apiMocks.fetchAgentRunDetail.mockResolvedValue({
      run: {
        id: 'run_1',
        workspace_id: 'ws_alpha',
        agent_id: 'hermes',
        model: 'Qwen/Qwen2.5-7B-Instruct',
        input: 'Inspect workers',
        status: 'succeeded',
        max_steps: 8,
        current_step: 2,
        final_output: 'Workers look healthy.',
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
          payload: {},
          created_at: '2026-03-31T12:00:01Z',
        },
        {
          id: 2,
          run_id: 'run_1',
          index: 1,
          type: 'final',
          payload: { message: 'Workers look healthy.' },
          created_at: '2026-03-31T12:00:02Z',
        },
      ],
    });
  });

  it('runs Hermes from the playground and renders the run trace', async () => {
    render(
      <PlaygroundProvider>
        <Playground />
      </PlaygroundProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'AGENT' }));
    fireEvent.change(
      screen.getByPlaceholderText(/Ask Hermes to inspect/i),
      { target: { value: 'Inspect workers' } },
    );
    fireEvent.click(screen.getByRole('button', { name: 'RUN AGENT' }));

    await waitFor(() => {
      expect(screen.getByText('Workers look healthy.')).toBeInTheDocument();
    });

    expect(apiMocks.createAgentRun).toHaveBeenCalledWith({
      agent_id: 'hermes',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Inspect workers',
      max_steps: 8,
    });
    expect(apiMocks.fetchAgentRunDetail).toHaveBeenCalledWith('run_1');
    expect(screen.getByText('RUN TRACE')).toBeInTheDocument();
    expect(screen.getAllByText(/list_workers/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText('SUCCEEDED').length).toBeGreaterThan(0);
  });
});
