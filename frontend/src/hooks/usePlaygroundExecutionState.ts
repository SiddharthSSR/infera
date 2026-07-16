import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';

import {
  cancelAgentRun,
  createAgentRun,
  fetchAgentRunDetail,
  uploadAgentAttachment,
} from '../lib/agentsClient';
import { streamChatCompletion } from '../lib/chatClient';
import type { PlaygroundHistoryEntry } from '../lib/chat-context';
import { formatAgentStatus } from '../lib/labels';
import type {
  AgentAnalysisDepth,
  AgentExecutionMode,
  AgentRunDetail,
  AgentRunStatus,
  PlaygroundMode,
} from '../types';

export interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  tokensPerSec: number;
}

const terminalAgentStatuses = new Set<AgentRunStatus>(['succeeded', 'failed', 'canceled']);

function promptPreview(prompt: string) {
  return prompt.slice(0, 50) + (prompt.length > 50 ? '...' : '');
}

export function summarizeAgentResult(detail: AgentRunDetail) {
  if (detail.run.final_output?.trim()) {
    return detail.run.final_output;
  }
  if (detail.run.status === 'failed') {
    return `Run failed: ${detail.run.failure_reason || 'Unknown error'}`;
  }
  if (detail.run.status === 'canceled') {
    return 'Run canceled.';
  }
  return 'Agent run completed without a final output.';
}

export function usePlaygroundExecutionState({
  setHistory,
  playgroundMode,
  setPlaygroundMode,
  agentExecutionMode,
  agentAnalysisDepth,
  selectedAgentID,
  agentMaxSteps,
  selectedModel,
  temperature,
  maxTokens,
}: {
  setHistory: React.Dispatch<React.SetStateAction<PlaygroundHistoryEntry[]>>;
  playgroundMode: PlaygroundMode;
  setPlaygroundMode: (mode: PlaygroundMode) => void;
  agentExecutionMode: AgentExecutionMode;
  agentAnalysisDepth: AgentAnalysisDepth;
  selectedAgentID: string;
  agentMaxSteps: number;
  selectedModel: string;
  temperature: number;
  maxTokens: number;
}) {
  const [prompt, setPrompt] = useState('');
  const [response, setResponse] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [topP, setTopP] = useState(1.0);
  const [freqPenalty, setFreqPenalty] = useState(0.0);
  const [tokenUsage, setTokenUsage] = useState<TokenUsage | null>(null);
  const [agentDetail, setAgentDetail] = useState<AgentRunDetail | null>(null);
  const [agentRunID, setAgentRunID] = useState('');
  const [screenshotFile, setScreenshotFile] = useState<File | null>(null);
  const [screenshotPreviewURL, setScreenshotPreviewURL] = useState('');
  const responseRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const agentPollTokenRef = useRef(0);
  const screenshotPreviewRef = useRef('');

  const isAgentMode = playgroundMode === 'agent';

  useEffect(() => {
    if (responseRef.current && isLoading) {
      responseRef.current.scrollTop = responseRef.current.scrollHeight;
    }
  }, [agentDetail, isLoading, response]);

  useEffect(() => () => {
    agentPollTokenRef.current += 1;
  }, []);

  useEffect(() => {
    if (!screenshotFile) {
      if (screenshotPreviewRef.current) {
        URL.revokeObjectURL(screenshotPreviewRef.current);
        screenshotPreviewRef.current = '';
        setScreenshotPreviewURL('');
      }
      return;
    }
    const preview = URL.createObjectURL(screenshotFile);
    screenshotPreviewRef.current = preview;
    setScreenshotPreviewURL(preview);
    return () => {
      if (screenshotPreviewRef.current === preview) {
        URL.revokeObjectURL(preview);
        screenshotPreviewRef.current = '';
      }
    };
  }, [screenshotFile]);

  const resetAgentRunState = useCallback(() => {
    agentPollTokenRef.current += 1;
    setAgentDetail(null);
    setAgentRunID('');
  }, []);

  const resetScreenshotState = useCallback(() => {
    setScreenshotFile(null);
    if (screenshotPreviewRef.current) {
      URL.revokeObjectURL(screenshotPreviewRef.current);
      screenshotPreviewRef.current = '';
    }
    setScreenshotPreviewURL('');
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  }, []);

  const handleChatRun = useCallback(async () => {
    if (!prompt.trim() || !selectedModel) {
      return;
    }

    setIsLoading(true);
    setResponse('');
    setTokenUsage(null);
    resetAgentRunState();
    const startTime = Date.now();

    try {
      const messages = [];
      if (systemPrompt.trim()) {
        messages.push({ role: 'system' as const, content: systemPrompt });
      }
      messages.push({ role: 'user' as const, content: prompt });

      const request = {
        model: selectedModel,
        messages,
        temperature,
        max_tokens: maxTokens,
        top_p: topP,
        frequency_penalty: freqPenalty,
      };

      let fullResponse = '';
      let streamingPromptTokens: number | undefined;
      let streamingCompletionTokens: number | undefined;

      for await (const chunk of streamChatCompletion(request, {
        onUsage: (usage) => {
          streamingPromptTokens = usage.prompt_tokens;
          streamingCompletionTokens = usage.completion_tokens;
        },
      })) {
        fullResponse += chunk;
        setResponse(fullResponse);
      }

      const latency = Date.now() - startTime;
      const completionTokens = streamingCompletionTokens ?? Math.round(fullResponse.split(/\s+/).length * 1.3);
      const promptTokens = streamingPromptTokens ?? Math.round(prompt.split(/\s+/).length * 1.3);
      const tokensPerSec = latency > 0 ? completionTokens / (latency / 1000) : 0;

      setTokenUsage({
        promptTokens,
        completionTokens,
        totalTokens: promptTokens + completionTokens,
        tokensPerSec,
      });

      setHistory((prev) => [{
        id: Math.random().toString(36).slice(2),
        time: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
        latencyMs: latency,
        preview: promptPreview(prompt),
        mode: 'chat' as const,
        promptTokens,
        completionTokens,
      }, ...prev].slice(0, 20));
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Request failed';
      setResponse(`Error: ${message}`);
      toast.error(message);
    } finally {
      setIsLoading(false);
    }
  }, [freqPenalty, maxTokens, prompt, resetAgentRunState, selectedModel, setHistory, systemPrompt, temperature, topP]);

  const ensureUploadedAttachments = useCallback(async () => {
    if (agentExecutionMode !== 'multimodal') {
      return [] as string[];
    }
    if (!screenshotFile) {
      throw new Error('Select a screenshot before running multimodal analysis');
    }
    const attachment = await uploadAgentAttachment(screenshotFile);
    return [attachment.id];
  }, [agentExecutionMode, screenshotFile]);

  const handleAgentRun = useCallback(async () => {
    if (!prompt.trim() || !selectedModel || !selectedAgentID) {
      return;
    }

    const pollToken = agentPollTokenRef.current + 1;
    agentPollTokenRef.current = pollToken;

    setIsLoading(true);
    setResponse('');
    setTokenUsage(null);
    setAgentDetail(null);
    setAgentRunID('');
    const startTime = Date.now();

    try {
      const attachments = await ensureUploadedAttachments();
      const run = await createAgentRun({
        agent_id: selectedAgentID,
        mode: agentExecutionMode,
        analysis_depth: agentAnalysisDepth,
        model: selectedModel,
        input: prompt,
        max_steps: agentMaxSteps,
        attachments,
      });
      if (agentPollTokenRef.current !== pollToken) {
        return;
      }

      setAgentRunID(run.id);
      setAgentDetail({ run, steps: [], attachments: [], sources: [] });

      while (true) {
        const nextDetail = await fetchAgentRunDetail(run.id);
        if (agentPollTokenRef.current !== pollToken) {
          return;
        }

        setAgentDetail(nextDetail);
        if (nextDetail.run.final_output?.trim()) {
          setResponse(nextDetail.run.final_output);
        }

        if (terminalAgentStatuses.has(nextDetail.run.status)) {
          const latency = Date.now() - startTime;
          setResponse(summarizeAgentResult(nextDetail));
          setHistory((prev) => [{
            id: Math.random().toString(36).slice(2),
            time: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
            latencyMs: latency,
            preview: promptPreview(prompt),
            mode: 'agent' as const,
            agentID: nextDetail.run.agent_id,
            statusLabel: formatAgentStatus(nextDetail.run.status),
          }, ...prev].slice(0, 20));

          if (nextDetail.run.status === 'failed') {
            toast.error(nextDetail.run.failure_reason || 'Agent run failed');
          } else if (nextDetail.run.status === 'canceled') {
            toast.success('Agent run canceled');
          }
          break;
        }

        await new Promise((resolve) => window.setTimeout(resolve, 1200));
        if (agentPollTokenRef.current !== pollToken) {
          return;
        }
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Agent run failed';
      setResponse(`Error: ${message}`);
      toast.error(message);
    } finally {
      if (agentPollTokenRef.current === pollToken) {
        setIsLoading(false);
      }
    }
  }, [
    agentAnalysisDepth,
    agentExecutionMode,
    agentMaxSteps,
    ensureUploadedAttachments,
    prompt,
    selectedAgentID,
    selectedModel,
    setHistory,
  ]);

  const handleRun = useCallback(async () => {
    if (isAgentMode) {
      await handleAgentRun();
      return;
    }
    await handleChatRun();
  }, [handleAgentRun, handleChatRun, isAgentMode]);

  const handleCancel = useCallback(async () => {
    if (!agentRunID) {
      return;
    }

    const pollToken = agentPollTokenRef.current + 1;
    agentPollTokenRef.current = pollToken;

    try {
      const run = await cancelAgentRun(agentRunID);
      let detail: AgentRunDetail = {
        run,
        steps: agentDetail?.steps || [],
        attachments: agentDetail?.attachments || [],
        sources: agentDetail?.sources || [],
      };

      try {
        detail = await fetchAgentRunDetail(agentRunID);
      } catch {
        // Best effort refresh.
      }

      if (agentPollTokenRef.current !== pollToken) {
        return;
      }

      setAgentDetail(detail);
      setResponse(summarizeAgentResult(detail));
      toast.success('Agent run canceled');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to cancel agent run';
      toast.error(message);
    } finally {
      if (agentPollTokenRef.current === pollToken) {
        setIsLoading(false);
      }
    }
  }, [agentDetail?.attachments, agentDetail?.sources, agentDetail?.steps, agentRunID]);

  const handleClear = useCallback(() => {
    resetAgentRunState();
    setPrompt('');
    setResponse('');
    setTokenUsage(null);
    setIsLoading(false);
    if (agentExecutionMode === 'multimodal') {
      resetScreenshotState();
    }
  }, [agentExecutionMode, resetAgentRunState, resetScreenshotState]);

  const handleModeChange = useCallback((mode: PlaygroundMode) => {
    if (mode === playgroundMode) {
      return;
    }
    resetAgentRunState();
    if (mode !== 'agent') {
      resetScreenshotState();
    }
    setPlaygroundMode(mode);
    setResponse('');
    setTokenUsage(null);
    setIsLoading(false);
  }, [playgroundMode, resetAgentRunState, resetScreenshotState, setPlaygroundMode]);

  const handleFileSelection = useCallback((file: File | null) => {
    setScreenshotFile(file);
  }, []);

  useEffect(() => {
    if (agentExecutionMode !== 'multimodal') {
      resetScreenshotState();
    }
  }, [agentExecutionMode, resetScreenshotState]);

  const canRun = Boolean(
    prompt.trim()
      && selectedModel
      && (!isAgentMode || selectedAgentID)
      && (agentExecutionMode !== 'multimodal' || screenshotFile),
  );

  return {
    prompt,
    setPrompt,
    response,
    systemPrompt,
    setSystemPrompt,
    isLoading,
    topP,
    setTopP,
    freqPenalty,
    setFreqPenalty,
    tokenUsage,
    agentDetail,
    agentRunID,
    screenshotFile,
    screenshotPreviewURL,
    responseRef,
    fileInputRef,
    canRun,
    handleRun,
    handleCancel,
    handleClear,
    handleModeChange,
    handleFileSelection,
  };
}
