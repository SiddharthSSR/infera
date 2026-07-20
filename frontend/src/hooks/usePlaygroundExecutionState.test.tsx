/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { AgentAnalysisDepth, AgentExecutionMode, PlaygroundHistoryEntry, PlaygroundMode } from '../types';
import { usePlaygroundExecutionState } from './usePlaygroundExecutionState';

const apiMocks = vi.hoisted(() => ({
  streamChatCompletion: vi.fn(),
  createAgentRun: vi.fn(),
  fetchAgentRunDetail: vi.fn(),
  cancelAgentRun: vi.fn(),
  uploadAgentAttachment: vi.fn(),
}));

const analyticsMocks = vi.hoisted(() => ({
  track: vi.fn(),
  trackFirst: vi.fn(),
}));

vi.mock('../lib/chatClient', () => ({
  streamChatCompletion: apiMocks.streamChatCompletion,
}));

vi.mock('../lib/agentsClient', () => ({
  createAgentRun: apiMocks.createAgentRun,
  fetchAgentRunDetail: apiMocks.fetchAgentRunDetail,
  cancelAgentRun: apiMocks.cancelAgentRun,
  uploadAgentAttachment: apiMocks.uploadAgentAttachment,
}));

vi.mock('../lib/publicAnalytics', () => ({
  publicAnalytics: analyticsMocks,
}));

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

type HookProps = {
  playgroundMode: PlaygroundMode;
  setPlaygroundMode: (mode: PlaygroundMode) => void;
  agentExecutionMode: AgentExecutionMode;
  agentAnalysisDepth: AgentAnalysisDepth;
  selectedAgentID: string;
  agentMaxSteps: number;
  selectedModel: string;
  temperature: number;
  maxTokens: number;
};

function renderExecutionState(overrides: Partial<HookProps> = {}) {
  const historyUpdates: Array<React.SetStateAction<PlaygroundHistoryEntry[]>> = [];
  const setHistory = vi.fn((update: React.SetStateAction<PlaygroundHistoryEntry[]>) => {
    historyUpdates.push(update);
  });
  const setPlaygroundMode = vi.fn();

  const result = renderHook(() => usePlaygroundExecutionState({
    setHistory,
    playgroundMode: overrides.playgroundMode ?? 'chat',
    setPlaygroundMode: overrides.setPlaygroundMode ?? setPlaygroundMode,
    agentExecutionMode: overrides.agentExecutionMode ?? 'operations',
    agentAnalysisDepth: overrides.agentAnalysisDepth ?? 'standard',
    selectedAgentID: overrides.selectedAgentID ?? 'hermes',
    agentMaxSteps: overrides.agentMaxSteps ?? 8,
    selectedModel: overrides.selectedModel ?? 'Qwen/Qwen2.5-7B-Instruct',
    temperature: overrides.temperature ?? 0.7,
    maxTokens: overrides.maxTokens ?? 2048,
  }));

  return {
    ...result,
    historyUpdates,
    setHistory,
    setPlaygroundMode,
  };
}

describe('usePlaygroundExecutionState', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('URL', {
      createObjectURL: vi.fn(() => 'blob:preview'),
      revokeObjectURL: vi.fn(),
    });
  });

  it('streams chat output and records token usage/history', async () => {
    apiMocks.streamChatCompletion.mockImplementation(async function* (_request: unknown, options?: { onUsage?: (usage: { prompt_tokens: number; completion_tokens: number }) => void }) {
      options?.onUsage?.({ prompt_tokens: 12, completion_tokens: 18 });
      yield 'chat ';
      yield 'output';
    });

    const { result, historyUpdates } = renderExecutionState();

    act(() => {
      result.current.setPrompt('Explain the workspace state');
      result.current.setSystemPrompt('Be concise');
    });

    await act(async () => {
      await result.current.handleRun();
    });

    expect(result.current.response).toBe('chat output');
    expect(apiMocks.streamChatCompletion).toHaveBeenCalledWith(expect.objectContaining({
      model: 'Qwen/Qwen2.5-7B-Instruct',
      temperature: 0.7,
      max_tokens: 2048,
      top_p: 1,
      frequency_penalty: 0,
    }), expect.any(Object));
    expect(result.current.tokenUsage).toEqual({
      promptTokens: 12,
      completionTokens: 18,
      totalTokens: 30,
      tokensPerSec: expect.any(Number),
    });
    expect(historyUpdates).toHaveLength(1);
    expect(analyticsMocks.trackFirst).toHaveBeenCalledWith(
      'activation_first_streaming_inference_succeeded',
      { surface: 'playground' },
    );
    const historyUpdate = historyUpdates[0];
    expect(typeof historyUpdate).toBe('function');
    const nextHistory = (historyUpdate as (prev: PlaygroundHistoryEntry[]) => PlaygroundHistoryEntry[])([]);
    expect(nextHistory[0]).toEqual(expect.objectContaining({
      mode: 'chat',
      preview: 'Explain the workspace state',
      promptTokens: 12,
      completionTokens: 18,
    }));
  });

  it('uploads multimodal screenshots before starting an agent run', async () => {
    apiMocks.uploadAgentAttachment.mockResolvedValue({ id: 'att_1' });
    apiMocks.createAgentRun.mockResolvedValue({
      id: 'run_1',
      workspace_id: 'ws_alpha',
      agent_id: 'hermes',
      mode: 'multimodal',
      analysis_depth: 'standard',
      model: 'Qwen/Qwen2.5-7B-Instruct',
      input: 'Check this screenshot',
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
        mode: 'multimodal',
        analysis_depth: 'standard',
        model: 'Qwen/Qwen2.5-7B-Instruct',
        input: 'Check this screenshot',
        status: 'succeeded',
        max_steps: 8,
        current_step: 0,
        final_output: 'Screenshot looks healthy.',
        created_at: '2026-03-31T12:00:00Z',
        updated_at: '2026-03-31T12:00:01Z',
      },
      steps: [],
      attachments: [],
      sources: [],
    });

    const file = new File(['test'], 'console.png', { type: 'image/png' });
    const { result } = renderExecutionState({
      playgroundMode: 'agent',
      agentExecutionMode: 'multimodal',
    });

    act(() => {
      result.current.setPrompt('Check this screenshot');
      result.current.handleFileSelection(file);
    });

    await act(async () => {
      await result.current.handleRun();
    });

    expect(apiMocks.uploadAgentAttachment).toHaveBeenCalledWith(file);
    expect(apiMocks.createAgentRun).toHaveBeenCalledWith(expect.objectContaining({
      mode: 'multimodal',
      attachments: ['att_1'],
    }));
    expect(result.current.response).toBe('Screenshot looks healthy.');
  });

  it('clears multimodal screenshots when switching away from agent mode', async () => {
    const { result, setPlaygroundMode } = renderExecutionState({
      playgroundMode: 'agent',
      agentExecutionMode: 'multimodal',
    });

    const file = new File(['test'], 'console.png', { type: 'image/png' });

    act(() => {
      result.current.handleFileSelection(file);
    });

    await waitFor(() => {
      expect(result.current.screenshotFile).toBe(file);
      expect(result.current.screenshotPreviewURL).toBe('blob:preview');
    });

    act(() => {
      result.current.handleModeChange('chat');
    });

    expect(setPlaygroundMode).toHaveBeenCalledWith('chat');
    expect(result.current.screenshotFile).toBeNull();
    expect(result.current.screenshotPreviewURL).toBe('');
  });
});
