import { useState, useRef, useCallback, useEffect, useMemo } from 'react';
import { toast } from 'sonner';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import { useAgents, useModels } from '../hooks/useApi';
import { CollapsibleSection } from '../components/CollapsibleSection';
import { useChat } from '../lib/chat-context';
import {
  cancelAgentRun,
  createAgentRun,
  fetchAgentRunDetail,
  streamChatCompletion,
  uploadAgentAttachment,
} from '../lib/api';
import type {
  AgentAttachment,
  AgentExecutionMode,
  AgentRunDetail,
  AgentRunStatus,
  AgentRunStep,
  PlaygroundMode,
} from '../types';

interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  tokensPerSec: number;
}

interface AgentThinkingState {
  headline: string;
  detail: string;
  recentChecks: string[];
}

const terminalAgentStatuses = new Set<AgentRunStatus>(['succeeded', 'failed', 'canceled']);

const toolLabelMap: Record<string, string> = {
  list_models: 'model availability',
  list_workers: 'worker health',
  get_gateway_stats: 'gateway pressure',
  list_instances: 'workspace instances',
  list_deployments: 'recent deployments',
  get_provider_status: 'provider connectivity',
  get_usage_summary: 'workspace usage',
  get_quota_status: 'quota pressure',
  web_search: 'official research',
  vision_analyze: 'screenshot review',
};

function formatAgentStatus(status?: AgentRunStatus) {
  return status ? status.replace(/_/g, ' ').toUpperCase() : 'IDLE';
}

function formatStepType(type: AgentRunStep['type']) {
  return type.replace(/_/g, ' ').toUpperCase();
}

function formatStepPayload(payload: unknown) {
  if (typeof payload === 'string') {
    return payload;
  }
  if (payload == null) {
    return '';
  }
  try {
    return JSON.stringify(payload, null, 2);
  } catch {
    return String(payload);
  }
}

function promptPreview(prompt: string) {
  return prompt.slice(0, 50) + (prompt.length > 50 ? '...' : '');
}

function summarizeAgentResult(detail: AgentRunDetail) {
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

function friendlyToolLabel(toolName?: string) {
  if (!toolName) {
    return 'workspace state';
  }
  return toolLabelMap[toolName] || toolName.replace(/_/g, ' ');
}

function toolAvailableInMode(modes: AgentExecutionMode[] | undefined, mode: AgentExecutionMode) {
  if (!modes || modes.length === 0) {
    return true;
  }
  return modes.includes(mode);
}

function latestStep(detail: AgentRunDetail | null) {
  if (!detail || detail.steps.length === 0) {
    return null;
  }
  return detail.steps[detail.steps.length - 1];
}

function deriveAgentThinking(detail: AgentRunDetail | null, isRunning: boolean): AgentThinkingState | null {
  if (!isRunning) {
    return null;
  }

  const step = latestStep(detail);
  const runStatus = detail?.run.status;
  let headline = 'Planning workspace review';
  let detailText = 'Hermes is preparing a safe review from the signals visible to your current key.';

  switch (runStatus) {
    case 'queued':
      headline = 'Planning workspace review';
      detailText = 'Hermes is deciding which safe checks to run first.';
      break;
    case 'running':
      if (!step) {
        headline = 'Inspecting workspace state';
        detailText = 'Hermes is gathering the first signals for this run.';
        break;
      }
      switch (step.type) {
        case 'tool_call':
          headline = `Checking ${friendlyToolLabel(step.tool_name)}`;
          detailText = 'Hermes is waiting for the latest read-only check to return.';
          break;
        case 'tool_result':
          headline = 'Cross-checking findings';
          detailText = 'Hermes is reconciling the latest result before it answers.';
          break;
        case 'error':
          headline = 'Recovering from a failed check';
          detailText = 'Hermes is continuing after a failed tool step without exposing raw reasoning.';
          break;
        default:
          headline = 'Inspecting workspace state';
          detailText = 'Hermes is gathering the first signals for this run.';
      }
      break;
    default:
      headline = 'Planning workspace review';
  }

  const recentChecks = (detail?.steps || [])
    .filter((entry) => entry.tool_name)
    .slice(-3)
    .map((entry) => friendlyToolLabel(entry.tool_name));

  return {
    headline,
    detail: detailText,
    recentChecks,
  };
}

function traceSummary(detail: AgentRunDetail | null) {
  if (!detail) {
    return 'No run trace available yet.';
  }
  if (detail.steps.length === 0) {
    return detail.run.status === 'queued'
      ? 'Queued with no steps yet.'
      : 'Waiting for the first structured step.';
  }

  const last = detail.steps[detail.steps.length - 1];
  const stepCount = `${detail.steps.length} step${detail.steps.length === 1 ? '' : 's'}`;
  if (last.tool_name) {
    return `${stepCount} · latest: ${friendlyToolLabel(last.tool_name)}`;
  }
  return `${stepCount} · latest: ${formatStepType(last.type).toLowerCase()}`;
}

function MarkdownOutput({ content }: { content: string }) {
  return (
    <div className="markdown-output">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          pre({ children, ...props }) {
            return (
              <pre
                {...props}
                style={{
                  background: '#F4F2EE',
                  border: '1px solid var(--border-color)',
                  padding: '1.25rem',
                  overflow: 'auto',
                  fontSize: '0.85rem',
                  lineHeight: 1.6,
                  marginBottom: '1rem',
                }}
              >
                {children}
              </pre>
            );
          },
          code({ className, children, ...props }) {
            const isInline = !className;
            if (isInline) {
              return (
                <code
                  {...props}
                  style={{
                    background: '#F4F2EE',
                    padding: '0.15rem 0.4rem',
                    fontSize: '0.88em',
                    fontFamily: 'var(--font-mono)',
                    border: '1px solid var(--border-color)',
                  }}
                >
                  {children}
                </code>
              );
            }
            return (
              <code className={className} {...props} style={{ fontFamily: 'var(--font-mono)' }}>
                {children}
              </code>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

export function Playground() {
  const {
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
  } = useChat();
  const { data: models } = useModels();
  const { data: agentsData, error: agentsError } = useAgents();
  const allModels = models || [];
  const agents = agentsData?.agents || [];

  const [prompt, setPrompt] = useState('');
  const [response, setResponse] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [topP, setTopP] = useState(1.0);
  const [freqPenalty, setFreqPenalty] = useState(0.0);
  const [tokenUsage, setTokenUsage] = useState<TokenUsage | null>(null);
  const [focusMode, setFocusMode] = useState(false);
  const [isMobile, setIsMobile] = useState(() => window.innerWidth <= 900);
  const [isCompactDesktop, setIsCompactDesktop] = useState(() => window.innerWidth <= 1460);
  const [showMobileSettings, setShowMobileSettings] = useState(false);
  const [showCompactHistory, setShowCompactHistory] = useState(false);
  const [agentDetail, setAgentDetail] = useState<AgentRunDetail | null>(null);
  const [agentRunID, setAgentRunID] = useState('');
  const [screenshotFile, setScreenshotFile] = useState<File | null>(null);
  const [screenshotPreviewURL, setScreenshotPreviewURL] = useState('');
  const [uploadedAttachment, setUploadedAttachment] = useState<AgentAttachment | null>(null);
  const responseRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const agentPollTokenRef = useRef(0);

  const isAgentMode = playgroundMode === 'agent';
  const agentModeAvailable = agents.length > 0;
  const activeAgent =
    agents.find((agent) => agent.id === selectedAgentID) ||
    agents.find((agent) => agent.id === agentsData?.default_agent_id) ||
    agents[0] ||
    null;
  const thinkingState = deriveAgentThinking(
    agentDetail,
    isAgentMode && (isLoading || agentDetail?.run.status === 'queued' || agentDetail?.run.status === 'running'),
  );

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && focusMode) {
        setFocusMode(false);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [focusMode]);

  useEffect(() => {
    const mediaQuery = window.matchMedia('(max-width: 900px)');
    const compactQuery = window.matchMedia('(max-width: 1460px)');
    const handleChange = (event: MediaQueryListEvent) => setIsMobile(event.matches);
    const handleCompactChange = (event: MediaQueryListEvent) => setIsCompactDesktop(event.matches);
    setIsMobile(mediaQuery.matches);
    setIsCompactDesktop(compactQuery.matches);
    mediaQuery.addEventListener('change', handleChange);
    compactQuery.addEventListener('change', handleCompactChange);
    return () => {
      mediaQuery.removeEventListener('change', handleChange);
      compactQuery.removeEventListener('change', handleCompactChange);
    };
  }, []);

  useEffect(() => {
    if (!isMobile) {
      setShowMobileSettings(false);
    }
    if (!isCompactDesktop) {
      setShowCompactHistory(false);
    }
  }, [isCompactDesktop, isMobile]);

  useEffect(() => {
    if (!selectedModel && allModels.length > 0) {
      setSelectedModel(allModels[0].id);
    }
  }, [allModels, selectedModel, setSelectedModel]);

  useEffect(() => {
    const defaultAgentID = agentsData?.default_agent_id || agents[0]?.id || '';
    if (!defaultAgentID) {
      return;
    }
    const selectedExists = agents.some((agent) => agent.id === selectedAgentID);
    if (!selectedAgentID || !selectedExists) {
      const nextAgent = agents.find((agent) => agent.id === defaultAgentID) || agents[0];
      setSelectedAgentID(nextAgent.id);
      setAgentMaxSteps(nextAgent.default_max_steps);
    }
  }, [agents, agentsData?.default_agent_id, selectedAgentID, setAgentMaxSteps, setSelectedAgentID]);

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
      if (screenshotPreviewURL) {
        URL.revokeObjectURL(screenshotPreviewURL);
        setScreenshotPreviewURL('');
      }
      return;
    }
    const preview = URL.createObjectURL(screenshotFile);
    setScreenshotPreviewURL(preview);
    return () => {
      URL.revokeObjectURL(preview);
    };
  }, [screenshotFile]);

  const resetAgentRunState = useCallback(() => {
    agentPollTokenRef.current += 1;
    setAgentDetail(null);
    setAgentRunID('');
  }, []);

  const resetScreenshotState = useCallback(() => {
    setScreenshotFile(null);
    setUploadedAttachment(null);
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
    if (uploadedAttachment) {
      return [uploadedAttachment.id];
    }
    const attachment = await uploadAgentAttachment(screenshotFile);
    setUploadedAttachment(attachment);
    return [attachment.id];
  }, [agentExecutionMode, screenshotFile, uploadedAttachment]);

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
    setUploadedAttachment(null);
    setScreenshotFile(file);
  }, []);

  const toolList = useMemo(() => activeAgent?.tools || [], [activeAgent?.tools]);
  const visibleToolList = useMemo(
    () => toolList.filter((tool) => toolAvailableInMode(tool.modes, agentExecutionMode)),
    [agentExecutionMode, toolList],
  );
  const canRun = Boolean(
    prompt.trim()
      && selectedModel
      && (!isAgentMode || (selectedAgentID && agentModeAvailable))
      && (agentExecutionMode !== 'multimodal' || screenshotFile),
  );

  useEffect(() => {
    if (agentExecutionMode !== 'multimodal') {
      resetScreenshotState();
    }
  }, [agentExecutionMode, resetScreenshotState]);

  const settingsControls = (
    <>
      <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>PLAY MODE</label>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem', marginBottom: '2rem' }}>
        <button
          className={playgroundMode === 'chat' ? 'btn-primary' : 'btn-secondary'}
          type="button"
          aria-pressed={playgroundMode === 'chat'}
          onClick={() => handleModeChange('chat')}
        >
          CHAT
        </button>
        <button
          className={playgroundMode === 'agent' ? 'btn-primary' : 'btn-secondary'}
          type="button"
          aria-pressed={playgroundMode === 'agent'}
          onClick={() => handleModeChange('agent')}
          disabled={!agentModeAvailable}
          title={!agentModeAvailable ? 'Agents are unavailable on this gateway' : 'Run a backend agent'}
        >
          AGENT
        </button>
      </div>

      <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>ACTIVE MODEL</label>
      <select
        value={selectedModel}
        onChange={(event) => setSelectedModel(event.target.value)}
        style={{
          width: '100%',
          padding: '0.75rem 0',
          background: 'transparent',
          border: 'none',
          borderBottom: '1px solid var(--text-primary)',
          fontFamily: 'var(--font-main)',
          fontSize: '1rem',
          outline: 'none',
          marginBottom: '2rem',
          cursor: 'pointer',
          color: 'var(--text-primary)',
        }}
      >
        {allModels.length === 0 && <option value="">No models available</option>}
        {allModels.map((model) => (
          <option key={model.id} value={model.id}>
            {model.id.split('/').pop()}
            {model.loaded === false ? ' (not loaded)' : ''}
            {model.parameters ? ` — ${model.parameters}` : ''}
          </option>
        ))}
      </select>

      {isAgentMode ? (
        <>
          <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>ACTIVE AGENT</label>
          <select
            value={selectedAgentID}
            onChange={(event) => {
              const nextAgent = agents.find((agent) => agent.id === event.target.value);
              setSelectedAgentID(event.target.value);
              if (nextAgent) {
                setAgentMaxSteps(nextAgent.default_max_steps);
              }
            }}
            style={{
              width: '100%',
              padding: '0.75rem 0',
              background: 'transparent',
              border: 'none',
              borderBottom: '1px solid var(--text-primary)',
              fontFamily: 'var(--font-main)',
              fontSize: '1rem',
              outline: 'none',
              marginBottom: '1rem',
              cursor: 'pointer',
              color: 'var(--text-primary)',
            }}
          >
            {agents.length === 0 && <option value="">No agents available</option>}
            {agents.map((agent) => (
              <option key={agent.id} value={agent.id}>
                {agent.name}
              </option>
            ))}
          </select>

          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: '1.5rem' }}>
            {activeAgent?.description || (agentsError instanceof Error ? agentsError.message : 'Agents are unavailable on this gateway.')}
          </div>

          <label className="label-text" style={{ marginBottom: '0.75rem', display: 'block' }}>AGENT MODE</label>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '0.5rem', marginBottom: '1.5rem' }}>
            {(['operations', 'research', 'multimodal'] as const).map((mode) => (
              <button
                key={mode}
                type="button"
                className={agentExecutionMode === mode ? 'btn-primary' : 'btn-secondary'}
                aria-pressed={agentExecutionMode === mode}
                onClick={() => setAgentExecutionMode(mode)}
              >
                {mode.toUpperCase()}
              </button>
            ))}
          </div>

          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: '1.5rem' }}>
            Tool availability follows the selected mode so the playground matches Hermes&apos; real backend contract.
          </div>

          <label className="label-text" style={{ marginBottom: '0.75rem', display: 'block' }}>ANALYSIS DEPTH</label>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem', marginBottom: '1.5rem' }}>
            <button
              type="button"
              className={agentAnalysisDepth === 'standard' ? 'btn-primary' : 'btn-secondary'}
              aria-pressed={agentAnalysisDepth === 'standard'}
              onClick={() => setAgentAnalysisDepth('standard')}
            >
              STANDARD
            </button>
            <button
              type="button"
              className={agentAnalysisDepth === 'deep' ? 'btn-primary' : 'btn-secondary'}
              aria-pressed={agentAnalysisDepth === 'deep'}
              onClick={() => setAgentAnalysisDepth('deep')}
            >
              DEEP
            </button>
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Max Steps</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{agentMaxSteps}</span>
            </div>
            <input
              type="range"
              min="1"
              max="16"
              step="1"
              value={agentMaxSteps}
              onChange={(event) => setAgentMaxSteps(parseInt(event.target.value, 10))}
            />
          </div>

          {agentExecutionMode === 'multimodal' && (
            <div style={{ marginBottom: '2rem' }}>
              <label className="label-text" style={{ marginBottom: '0.85rem', display: 'block' }}>SCREENSHOT</label>
              <input
                ref={fileInputRef}
                type="file"
                aria-label="Screenshot upload"
                accept="image/png,image/jpeg,image/webp"
                style={{ display: 'none' }}
                onChange={(event) => handleFileSelection(event.target.files?.[0] || null)}
              />
              <button
                type="button"
                className="btn-secondary"
                style={{ width: '100%', marginBottom: '0.75rem' }}
                onClick={() => fileInputRef.current?.click()}
              >
                {screenshotFile ? 'REPLACE SCREENSHOT' : 'UPLOAD SCREENSHOT'}
              </button>
              {screenshotFile && (
                <div style={{ display: 'grid', gap: '0.65rem' }}>
                  <div className="mono" style={{ fontSize: '0.68rem', color: 'var(--text-secondary)' }}>
                    {screenshotFile.name} · {(screenshotFile.size / 1024).toFixed(1)} KB
                  </div>
                  {screenshotPreviewURL && (
                    <img
                      src={screenshotPreviewURL}
                      alt="Selected screenshot preview"
                      style={{
                        width: '100%',
                        border: '1px solid var(--border-color)',
                        background: '#F4F2EE',
                        objectFit: 'cover',
                      }}
                    />
                  )}
                </div>
              )}
            </div>
          )}

          <div style={{ marginTop: '2rem' }}>
            <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>AVAILABLE TOOLS</label>
            <div style={{ display: 'grid', gap: '0.75rem' }}>
              {visibleToolList.map((tool) => (
                <div
                  key={tool.name}
                  style={{
                    paddingBottom: '0.75rem',
                    borderBottom: '1px solid var(--border-color)',
                  }}
                >
                  <div className="mono" style={{ fontSize: '0.72rem', marginBottom: '0.35rem' }}>{tool.name}</div>
                  <div style={{ fontSize: '0.82rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>{tool.description}</div>
                </div>
              ))}
              {activeAgent && toolList.length > 0 && visibleToolList.length === 0 && (
                <div style={{ fontSize: '0.82rem', color: 'var(--text-secondary)' }}>
                  No tools are available for the selected mode with your current key permissions.
                </div>
              )}
              {activeAgent && toolList.length === 0 && (
                <div style={{ fontSize: '0.82rem', color: 'var(--text-secondary)' }}>
                  This agent does not expose any tools for your current key permissions.
                </div>
              )}
            </div>
          </div>
        </>
      ) : (
        <>
          <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>PARAMETERS</label>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Temperature</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{temperature.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="2" step="0.01" value={temperature} onChange={(event) => setTemperature(parseFloat(event.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Max Tokens</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{maxTokens}</span>
            </div>
            <input type="range" min="1" max="8192" step="1" value={maxTokens} onChange={(event) => setMaxTokens(parseInt(event.target.value, 10))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Top P</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{topP.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="1" step="0.01" value={topP} onChange={(event) => setTopP(parseFloat(event.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Frequency Penalty</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{freqPenalty.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="2" step="0.01" value={freqPenalty} onChange={(event) => setFreqPenalty(parseFloat(event.target.value))} />
          </div>

          <div style={{ marginTop: '2rem' }}>
            <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>SYSTEM PROMPT</label>
            <textarea
              value={systemPrompt}
              onChange={(event) => setSystemPrompt(event.target.value)}
              placeholder="You are a helpful assistant..."
              style={{
                width: '100%',
                height: 120,
                border: 'none',
                background: 'transparent',
                resize: 'none',
                outline: 'none',
                fontFamily: 'var(--font-main)',
                fontSize: '0.85rem',
                lineHeight: 1.6,
                color: 'var(--text-primary)',
                borderBottom: '1px solid var(--border-color)',
              }}
            />
          </div>
        </>
      )}
    </>
  );

  const historyPanel = (
    <>
      {history.length === 0 ? (
        <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', padding: '1rem 0' }}>
          No requests yet. Run an inference or agent task to see history.
        </div>
      ) : (
        history.map((entry, index) => (
          <button
            type="button"
            key={entry.id}
            style={{
              padding: '1rem 0',
              cursor: 'pointer',
              opacity: index === 0 ? 1 : 0.7,
              background: 'none',
              border: 'none',
              borderBottom: '1px solid #E5E2DE',
              width: '100%',
              textAlign: 'left',
            }}
          >
            <span className="mono" style={{ fontSize: '0.65rem', color: 'var(--text-secondary)', display: 'block', marginBottom: '0.25rem' }}>
              {entry.time} - {entry.latencyMs}ms
            </span>
            <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.35rem', flexWrap: 'wrap' }}>
              <span className="mono" style={{ fontSize: '0.62rem', color: 'var(--text-secondary)' }}>
                {entry.mode === 'agent' ? (entry.agentID || 'agent').toUpperCase() : 'CHAT'}
              </span>
              {entry.statusLabel && (
                <span className="mono" style={{ fontSize: '0.62rem', color: 'var(--text-secondary)' }}>
                  {entry.statusLabel}
                </span>
              )}
            </div>
            <div
              style={{
                fontSize: '0.85rem',
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                color: 'var(--text-primary)',
              }}
            >
              {entry.preview}
            </div>
            {entry.promptTokens != null && (
              <span className="mono" style={{ fontSize: '0.6rem', color: 'var(--text-secondary)', marginTop: '0.25rem', display: 'block' }}>
                {entry.promptTokens} + {entry.completionTokens} tokens
              </span>
            )}
          </button>
        ))
      )}

      {history.length > 0 && (
        <div style={{ marginTop: '1rem' }}>
          <button
            className="btn-secondary"
            style={{ width: '100%', borderStyle: 'dashed', opacity: 0.5 }}
            onClick={() => setHistory([])}
          >
            CLEAR HISTORY
          </button>
        </div>
      )}
    </>
  );

  const showDesktopSettingsRail = !focusMode && !isMobile && !isCompactDesktop;
  const showDesktopHistoryRail = showDesktopSettingsRail && history.length > 0;
  const playgroundGridTemplateColumns = focusMode || isMobile || isCompactDesktop
    ? '1fr'
    : showDesktopHistoryRail
      ? '252px minmax(0, 1fr) 236px'
      : '252px minmax(0, 1fr)';

  const runStatusText = isAgentMode
    ? (agentDetail?.run.status
      ? `agent ${formatAgentStatus(agentDetail.run.status).toLowerCase()}`
      : (isLoading ? 'starting agent run...' : (activeAgent ? `${activeAgent.name.toLowerCase()} ready` : 'agent mode unavailable')))
    : (isLoading ? 'generating...' : 'ready to inference');

  const terminalRun = agentDetail && terminalAgentStatuses.has(agentDetail.run.status);
  const traceExpandedByDefault = agentDetail?.run.status === 'failed' || agentDetail?.run.status === 'canceled';

  return (
    <div
      style={focusMode ? {
        position: 'fixed',
        inset: 0,
        zIndex: 100,
        background: 'var(--bg-paper)',
        display: 'flex',
        flexDirection: 'column',
      } : {}}
    >
      {!focusMode && (
        <header className="display-text" style={{ fontSize: isMobile ? '3rem' : '4.2rem', padding: isMobile ? '1.25rem 0' : '1.5rem 0 1.2rem' }}>
          PLAYGROUND
        </header>
      )}

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: playgroundGridTemplateColumns,
          flexGrow: 1,
          overflow: 'hidden',
          height: focusMode ? '100vh' : isMobile ? 'calc(100vh - 170px)' : 'calc(100vh - 190px)',
        }}
      >
        {showDesktopSettingsRail && (
          <aside
            style={{
              padding: '1.5rem',
              borderRight: 'var(--grid-line)',
              overflowY: 'auto',
            }}
          >
            {settingsControls}
          </aside>
        )}

        <main style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div
            style={{
              padding: isMobile ? '1rem' : '1rem 1.5rem',
              borderBottom: 'var(--grid-line)',
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              flexWrap: 'wrap',
              gap: '0.75rem',
              flexShrink: 0,
            }}
          >
            <div style={{ display: 'grid', gap: '0.35rem' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: '0.65rem', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-secondary)' }}>
                <div style={{ width: 6, height: 6, background: isLoading ? 'var(--color-warning)' : 'var(--color-success)', borderRadius: '50%' }} />
                {runStatusText}
              </div>
              {!showDesktopHistoryRail && !isMobile && !isCompactDesktop && (
                <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  {isAgentMode
                    ? 'Hermes runs asynchronously and the playground derives progress from structured run steps.'
                    : 'Request history will appear here after the first successful run.'}
                </div>
              )}
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
              {(isMobile || isCompactDesktop) && !focusMode && (
                <button className="btn-secondary" onClick={() => setShowMobileSettings((value) => !value)}>
                  {showMobileSettings ? 'HIDE SETTINGS' : 'SETTINGS'}
                </button>
              )}
              {(isMobile || isCompactDesktop) && !focusMode && (
                <button className="btn-secondary" onClick={() => setShowCompactHistory((value) => !value)}>
                  {showCompactHistory ? 'HIDE HISTORY' : 'HISTORY'}
                </button>
              )}
              {isAgentMode && isLoading && agentRunID && (
                <button className="btn-secondary" onClick={handleCancel}>
                  CANCEL RUN
                </button>
              )}
              <button className="btn-secondary" onClick={() => setFocusMode((value) => !value)}>
                {focusMode ? 'EXIT' : 'FOCUS'}
              </button>
              <button className="btn-secondary" onClick={handleClear}>CLEAR</button>
              <button className="btn-primary" onClick={handleRun} disabled={isLoading || !canRun}>
                {isLoading ? (isAgentMode ? 'RUNNING AGENT...' : 'GENERATING...') : isAgentMode ? 'RUN AGENT' : 'RUN INFERENCE'}
              </button>
            </div>
          </div>

          {(isMobile || isCompactDesktop) && !focusMode && showMobileSettings && (
            <section
              style={{
                padding: '1rem',
                borderBottom: 'var(--grid-line)',
                backgroundColor: 'var(--bg-accent)',
              }}
            >
              <CollapsibleSection
                title="SETTINGS"
                description={isAgentMode ? 'Mode selection, agent controls, screenshots, and workspace-safe tools.' : 'Model selection, decoding controls, and system prompt.'}
                defaultExpanded
              >
                {settingsControls}
              </CollapsibleSection>
            </section>
          )}

          {(isMobile || isCompactDesktop) && !focusMode && showCompactHistory && (
            <section
              style={{
                padding: '1rem',
                borderBottom: 'var(--grid-line)',
                backgroundColor: 'rgba(244, 242, 238, 0.72)',
              }}
            >
              <CollapsibleSection
                title="REQUEST HISTORY"
                description="Recent prompts, latency, and execution results."
                defaultExpanded
              >
                {historyPanel}
              </CollapsibleSection>
            </section>
          )}

          <section
            style={{
              padding: isMobile ? '1rem' : '1.25rem 1.5rem',
              borderBottom: 'var(--grid-line)',
              display: 'flex',
              flexDirection: 'column',
              height: 132,
              flexShrink: 0,
            }}
          >
            <label className="label-text" style={{ marginBottom: '0.75rem', display: 'block' }}>
              {isAgentMode ? 'TASK' : 'USER PROMPT'}
            </label>
            <textarea
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder={isAgentMode
                ? agentExecutionMode === 'research'
                  ? 'Ask Hermes to investigate and cite official sources...'
                  : agentExecutionMode === 'multimodal'
                    ? 'Ask Hermes what this screenshot shows and what it implies for the workspace...'
                    : 'Ask Hermes to inspect workspace health, quota pressure, deployments, or provider issues...'
                : 'Type your instruction here...'}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && event.metaKey) {
                  void handleRun();
                }
              }}
              style={{
                width: '100%',
                flex: 1,
                border: 'none',
                background: 'transparent',
                resize: 'none',
                outline: 'none',
                fontFamily: 'var(--font-main)',
                fontSize: isMobile ? '0.95rem' : '1.05rem',
                lineHeight: 1.6,
                color: 'var(--text-primary)',
              }}
            />
          </section>

          <section
            style={{
              flex: 1,
              display: 'flex',
              flexDirection: 'column',
              overflow: 'hidden',
              minHeight: 0,
            }}
          >
            <div
              style={{
                padding: isMobile ? '0.75rem 1rem 0.5rem' : '0.85rem 1.5rem 0.4rem',
                flexShrink: 0,
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                flexWrap: 'wrap',
                gap: '0.5rem',
              }}
            >
              <label className="label-text">OUTPUT</label>
              {tokenUsage && !isAgentMode && (
                <div className="mono" style={{ fontSize: '0.65rem', color: 'var(--text-secondary)', display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
                  <span>{tokenUsage.promptTokens} prompt</span>
                  <span>{tokenUsage.completionTokens} completion</span>
                  <span>{tokenUsage.totalTokens} total</span>
                  <span>{tokenUsage.tokensPerSec.toFixed(1)} tok/s</span>
                </div>
              )}
            </div>

            <div
              ref={responseRef}
              style={{
                flex: 1,
                overflowY: 'auto',
                padding: isMobile ? '0.5rem 1rem 1rem' : '0.45rem 1.5rem 1.5rem',
                minHeight: 0,
              }}
            >
              {isAgentMode ? (
                <>
                  {thinkingState ? (
                    <section className="animate-fade-in" style={{ border: '1px solid var(--border-color)', padding: '1.25rem', background: 'rgba(244, 242, 238, 0.7)', marginBottom: '1rem' }}>
                      <div className="mono" style={{ fontSize: '0.68rem', marginBottom: '0.6rem', color: 'var(--text-secondary)' }}>LIVE REVIEW</div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '0.65rem' }}>
                        <div style={{ display: 'flex', gap: '0.35rem' }} aria-hidden="true">
                          <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-primary)', opacity: 0.35 }} />
                          <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-primary)', opacity: 0.55 }} />
                          <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-primary)', opacity: 0.75 }} />
                        </div>
                        <div style={{ fontSize: '1.1rem', fontWeight: 600 }}>{thinkingState.headline}</div>
                      </div>
                      <p style={{ color: 'var(--text-secondary)', lineHeight: 1.7, marginBottom: '0.85rem' }}>{thinkingState.detail}</p>
                      <div className="mono" style={{ fontSize: '0.68rem', color: 'var(--text-secondary)', display: 'flex', gap: '0.85rem', flexWrap: 'wrap', marginBottom: thinkingState.recentChecks.length > 0 ? '0.75rem' : 0 }}>
                        <span>{agentExecutionMode.toUpperCase()}</span>
                        <span>{agentAnalysisDepth.toUpperCase()}</span>
                        {agentDetail?.run.current_step != null && <span>STEP {agentDetail.run.current_step + 1}</span>}
                      </div>
                      {thinkingState.recentChecks.length > 0 && (
                        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                          {thinkingState.recentChecks.map((label, index) => (
                            <span key={`${label}:${index}`} className="mono" style={{ fontSize: '0.64rem', padding: '0.25rem 0.5rem', border: '1px solid var(--border-color)', background: 'white' }}>
                              {label}
                            </span>
                          ))}
                        </div>
                      )}
                    </section>
                  ) : terminalRun && agentDetail ? (
                    <section className="animate-fade-in" style={{ border: '1px solid var(--border-color)', padding: '1.25rem', background: agentDetail.run.status === 'succeeded' ? 'white' : 'rgba(255, 246, 242, 0.9)', marginBottom: '1rem' }}>
                      <div className="mono" style={{ fontSize: '0.68rem', marginBottom: '0.75rem', color: 'var(--text-secondary)', display: 'flex', gap: '0.85rem', flexWrap: 'wrap' }}>
                        <span>{formatAgentStatus(agentDetail.run.status)}</span>
                        <span>{agentExecutionMode.toUpperCase()}</span>
                        <span>{agentAnalysisDepth.toUpperCase()}</span>
                        <span>{agentDetail.steps.length} STEP{agentDetail.steps.length === 1 ? '' : 'S'}</span>
                      </div>
                      <MarkdownOutput content={summarizeAgentResult(agentDetail)} />
                      {agentDetail.sources && agentDetail.sources.length > 0 && (
                        <div style={{ marginTop: '1rem', display: 'grid', gap: '0.75rem' }}>
                          <div className="label-text">SOURCES</div>
                          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                            {agentDetail.sources.map((source) => (
                              <a
                                key={source.url}
                                href={source.url}
                                target="_blank"
                                rel="noreferrer"
                                className="mono"
                                style={{
                                  fontSize: '0.66rem',
                                  padding: '0.32rem 0.55rem',
                                  border: '1px solid var(--border-color)',
                                  background: '#F4F2EE',
                                  color: 'var(--text-primary)',
                                  textDecoration: 'none',
                                }}
                              >
                                {source.domain}
                              </a>
                            ))}
                          </div>
                        </div>
                      )}
                    </section>
                  ) : response ? (
                    <section className="animate-fade-in" style={{ border: '1px solid var(--border-color)', padding: '1.25rem', background: 'white', marginBottom: '1rem' }}>
                      <MarkdownOutput content={response} />
                    </section>
                  ) : (
                    <span style={{ color: 'var(--text-secondary)' }}>
                      Hermes will surface a narrative answer here once the run starts.
                    </span>
                  )}

                  {agentDetail && (
                    <div style={{ marginTop: '1rem' }}>
                      <CollapsibleSection
                        key={`${agentDetail.run.id}:${agentDetail.run.status}`}
                        title="RUN TRACE"
                        description="Structured tool calls, results, and terminal events from the backend runtime."
                        summary={traceSummary(agentDetail)}
                        defaultExpanded={traceExpandedByDefault}
                      >
                        {agentDetail.steps.length === 0 ? (
                          <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
                            No structured steps yet.
                          </div>
                        ) : (
                          <div style={{ display: 'grid', gap: '1rem' }}>
                            {agentDetail.steps.map((step) => (
                              <div key={step.id} style={{ borderBottom: '1px solid var(--border-color)', paddingBottom: '0.9rem' }}>
                                <div className="mono" style={{ fontSize: '0.68rem', marginBottom: '0.35rem', color: 'var(--text-secondary)' }}>
                                  STEP {step.index + 1} · {formatStepType(step.type)}{step.tool_name ? ` · ${step.tool_name}` : ''}
                                </div>
                                <pre
                                  style={{
                                    margin: 0,
                                    background: '#F4F2EE',
                                    border: '1px solid var(--border-color)',
                                    padding: '0.85rem',
                                    overflowX: 'auto',
                                    whiteSpace: 'pre-wrap',
                                    wordBreak: 'break-word',
                                    fontFamily: 'var(--font-mono)',
                                    fontSize: '0.78rem',
                                    lineHeight: 1.6,
                                  }}
                                >
                                  {formatStepPayload(step.payload)}
                                </pre>
                              </div>
                            ))}
                          </div>
                        )}
                      </CollapsibleSection>
                    </div>
                  )}
                </>
              ) : response ? (
                <div className="animate-fade-in">
                  <MarkdownOutput content={response} />
                </div>
              ) : (
                <span style={{ color: 'var(--text-secondary)' }}>
                  Model output will appear here.
                </span>
              )}
            </div>
          </section>
        </main>

        {showDesktopHistoryRail && (
          <aside
            style={{
              padding: '1.5rem',
              borderLeft: 'var(--grid-line)',
              overflowY: 'auto',
            }}
          >
            <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>REQUEST HISTORY</label>
            {historyPanel}
          </aside>
        )}
      </div>
    </div>
  );
}
