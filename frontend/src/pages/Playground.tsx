import { useState, useRef, useCallback, useEffect } from 'react';
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
} from '../lib/api';
import type { AgentRunDetail, AgentRunStatus, AgentRunStep } from '../types';

interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  tokensPerSec: number;
}

const terminalAgentStatuses = new Set<AgentRunStatus>(['succeeded', 'failed', 'canceled']);

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

function promptPreview(prompt: string) {
  return prompt.slice(0, 50) + (prompt.length > 50 ? '...' : '');
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
  const responseRef = useRef<HTMLDivElement>(null);
  const agentPollTokenRef = useRef(0);

  const isAgentMode = playgroundMode === 'agent';
  const agentModeAvailable = agents.length > 0;
  const activeAgent =
    agents.find((agent) => agent.id === selectedAgentID) ||
    agents.find((agent) => agent.id === agentsData?.default_agent_id) ||
    agents[0] ||
    null;

  // Keyboard shortcut: Escape to exit focus mode
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && focusMode) setFocusMode(false);
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

  const resetAgentRunState = useCallback(() => {
    agentPollTokenRef.current += 1;
    setAgentDetail(null);
    setAgentRunID('');
  }, []);

  const handleChatRun = useCallback(async () => {
    if (!prompt.trim() || !selectedModel) return;
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
      const tokensPerSec = latency > 0 ? (completionTokens / (latency / 1000)) : 0;

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
      const msg = err instanceof Error ? err.message : 'Request failed';
      setResponse(`Error: ${msg}`);
      toast.error(msg);
    } finally {
      setIsLoading(false);
    }
  }, [
    freqPenalty,
    maxTokens,
    prompt,
    resetAgentRunState,
    selectedModel,
    setHistory,
    systemPrompt,
    temperature,
    topP,
  ]);

  const handleAgentRun = useCallback(async () => {
    if (!prompt.trim() || !selectedModel || !selectedAgentID) return;

    const pollToken = agentPollTokenRef.current + 1;
    agentPollTokenRef.current = pollToken;

    setIsLoading(true);
    setResponse('');
    setTokenUsage(null);
    setAgentDetail(null);
    setAgentRunID('');
    const startTime = Date.now();

    try {
      const run = await createAgentRun({
        agent_id: selectedAgentID,
        model: selectedModel,
        input: prompt,
        max_steps: agentMaxSteps,
      });
      if (agentPollTokenRef.current !== pollToken) return;

      setAgentRunID(run.id);

      while (true) {
        const nextDetail = await fetchAgentRunDetail(run.id);
        if (agentPollTokenRef.current !== pollToken) return;

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
        if (agentPollTokenRef.current !== pollToken) return;
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Agent run failed';
      setResponse(`Error: ${msg}`);
      toast.error(msg);
    } finally {
      if (agentPollTokenRef.current === pollToken) {
        setIsLoading(false);
      }
    }
  }, [agentMaxSteps, prompt, selectedAgentID, selectedModel, setHistory]);

  const handleRun = useCallback(async () => {
    if (isAgentMode) {
      await handleAgentRun();
      return;
    }
    await handleChatRun();
  }, [handleAgentRun, handleChatRun, isAgentMode]);

  const handleCancel = useCallback(async () => {
    if (!agentRunID) return;

    const pollToken = agentPollTokenRef.current + 1;
    agentPollTokenRef.current = pollToken;

    try {
      const run = await cancelAgentRun(agentRunID);
      let detail: AgentRunDetail = {
        run,
        steps: agentDetail?.steps || [],
      };

      try {
        detail = await fetchAgentRunDetail(agentRunID);
      } catch {
        // Best effort; fall back to cancel response.
      }

      if (agentPollTokenRef.current !== pollToken) return;
      setAgentDetail(detail);
      setResponse(summarizeAgentResult(detail));
      toast.success('Agent run canceled');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to cancel agent run';
      toast.error(msg);
    } finally {
      if (agentPollTokenRef.current === pollToken) {
        setIsLoading(false);
      }
    }
  }, [agentDetail?.steps, agentRunID]);

  const handleClear = useCallback(() => {
    resetAgentRunState();
    setPrompt('');
    setResponse('');
    setTokenUsage(null);
    setIsLoading(false);
  }, [resetAgentRunState]);

  const handleModeChange = useCallback((mode: 'chat' | 'agent') => {
    if (mode === playgroundMode) {
      return;
    }
    resetAgentRunState();
    setPlaygroundMode(mode);
    setResponse('');
    setTokenUsage(null);
    setIsLoading(false);
  }, [playgroundMode, resetAgentRunState, setPlaygroundMode]);

  const canRun = Boolean(
    prompt.trim()
    && selectedModel
    && (!isAgentMode || (selectedAgentID && agentModeAvailable)),
  );

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
        onChange={(e) => setSelectedModel(e.target.value)}
        style={{
          width: '100%', padding: '0.75rem 0', background: 'transparent',
          border: 'none', borderBottom: '1px solid var(--text-primary)',
          fontFamily: 'var(--font-main)', fontSize: '1rem', outline: 'none',
          marginBottom: '2rem', cursor: 'pointer', color: 'var(--text-primary)',
        }}
      >
        {allModels.length === 0 && <option value="">No models available</option>}
        {allModels.map((model) => (
          <option key={model.id} value={model.id}>
            {model.id.split('/').pop()}{model.loaded === false ? ' (not loaded)' : ''}{model.parameters ? ` — ${model.parameters}` : ''}
          </option>
        ))}
      </select>

      {isAgentMode ? (
        <>
          <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>ACTIVE AGENT</label>
          <select
            value={selectedAgentID}
            onChange={(e) => {
              const nextAgent = agents.find((agent) => agent.id === e.target.value);
              setSelectedAgentID(e.target.value);
              if (nextAgent) {
                setAgentMaxSteps(nextAgent.default_max_steps);
              }
            }}
            style={{
              width: '100%', padding: '0.75rem 0', background: 'transparent',
              border: 'none', borderBottom: '1px solid var(--text-primary)',
              fontFamily: 'var(--font-main)', fontSize: '1rem', outline: 'none',
              marginBottom: '1rem', cursor: 'pointer', color: 'var(--text-primary)',
            }}
          >
            {agents.length === 0 && <option value="">No agents available</option>}
            {agents.map((agent) => (
              <option key={agent.id} value={agent.id}>
                {agent.name}
              </option>
            ))}
          </select>

          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: '2rem' }}>
            {activeAgent?.description || (agentsError instanceof Error ? agentsError.message : 'Agents are unavailable on this gateway.')}
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
              onChange={(e) => setAgentMaxSteps(parseInt(e.target.value))}
            />
          </div>

          <div style={{ marginTop: '2rem' }}>
            <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>AVAILABLE TOOLS</label>
            <div style={{ display: 'grid', gap: '0.75rem' }}>
              {(activeAgent?.tools || []).map((tool) => (
                <div key={tool.name} style={{
                  paddingBottom: '0.75rem',
                  borderBottom: '1px solid var(--border-color)',
                }}>
                  <div className="mono" style={{ fontSize: '0.72rem', marginBottom: '0.35rem' }}>{tool.name}</div>
                  <div style={{ fontSize: '0.82rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>{tool.description}</div>
                </div>
              ))}
              {activeAgent && activeAgent.tools.length === 0 && (
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
            <input type="range" min="0" max="2" step="0.01" value={temperature}
              onChange={(e) => setTemperature(parseFloat(e.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Max Tokens</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{maxTokens}</span>
            </div>
            <input type="range" min="1" max="8192" step="1" value={maxTokens}
              onChange={(e) => setMaxTokens(parseInt(e.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Top P</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{topP.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="1" step="0.01" value={topP}
              onChange={(e) => setTopP(parseFloat(e.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Frequency Penalty</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{freqPenalty.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="2" step="0.01" value={freqPenalty}
              onChange={(e) => setFreqPenalty(parseFloat(e.target.value))} />
          </div>

          <div style={{ marginTop: '2rem' }}>
            <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>SYSTEM PROMPT</label>
            <textarea
              value={systemPrompt}
              onChange={(e) => setSystemPrompt(e.target.value)}
              placeholder="You are a helpful assistant..."
              style={{
                width: '100%', height: 120, border: 'none', background: 'transparent',
                resize: 'none', outline: 'none', fontFamily: 'var(--font-main)',
                fontSize: '0.85rem', lineHeight: 1.6, color: 'var(--text-primary)',
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
        history.map((entry, i) => (
          <button
            type="button"
            key={entry.id}
            style={{
              padding: '1rem 0',
              cursor: 'pointer',
              opacity: i === 0 ? 1 : 0.7,
              background: 'none',
              border: 'none',
              borderBottom: '1px solid #E5E2DE',
              width: '100%',
              textAlign: 'left',
            }}
            onClick={() => {
              // Reserved for future prompt restore.
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
            <div style={{
              fontSize: '0.85rem', whiteSpace: 'nowrap', overflow: 'hidden',
              textOverflow: 'ellipsis', color: 'var(--text-primary)',
            }}>
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
    ? (agentDetail?.run.status ? `agent ${formatAgentStatus(agentDetail.run.status).toLowerCase()}` : (isLoading ? 'starting agent run...' : (activeAgent ? `${activeAgent.name.toLowerCase()} ready` : 'agent mode unavailable')))
    : (isLoading ? 'generating...' : 'ready to inference');

  return (
    <div style={focusMode ? {
      position: 'fixed', inset: 0, zIndex: 100,
      background: 'var(--bg-paper)',
      display: 'flex', flexDirection: 'column',
    } : {}}>
      {!focusMode && (
        <header className="display-text" style={{ fontSize: isMobile ? '3rem' : '4.2rem', padding: isMobile ? '1.25rem 0' : '1.5rem 0 1.2rem' }}>
          PLAYGROUND
        </header>
      )}

      <div style={{
        display: 'grid',
        gridTemplateColumns: playgroundGridTemplateColumns,
        flexGrow: 1,
        overflow: 'hidden',
        height: focusMode ? '100vh' : isMobile ? 'calc(100vh - 170px)' : 'calc(100vh - 190px)',
      }}>
        {showDesktopSettingsRail && <aside style={{
          padding: '1.5rem',
          borderRight: 'var(--grid-line)',
          overflowY: 'auto',
        }}>
          {settingsControls}
        </aside>}

        <main style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{
            padding: isMobile ? '1rem' : '1rem 1.5rem', borderBottom: 'var(--grid-line)',
            display: 'flex', justifyContent: 'space-between', alignItems: 'center',
            flexWrap: 'wrap', gap: '0.75rem',
            flexShrink: 0,
          }}>
            <div style={{ display: 'grid', gap: '0.35rem' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: '0.65rem', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-secondary)' }}>
                <div style={{ width: 6, height: 6, background: isLoading ? 'var(--color-warning)' : 'var(--color-success)', borderRadius: '50%' }} />
                {runStatusText}
              </div>
              {!showDesktopHistoryRail && !isMobile && !isCompactDesktop && (
                <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  {isAgentMode
                    ? 'Agent runs are asynchronous and polled from the backend runtime.'
                    : 'Request history will appear here after the first successful run.'}
                </div>
              )}
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
              {(isMobile || isCompactDesktop) && !focusMode && (
                <button
                  className="btn-secondary"
                  onClick={() => setShowMobileSettings((prev) => !prev)}
                >
                  {showMobileSettings ? 'HIDE SETTINGS' : 'SETTINGS'}
                </button>
              )}
              {(isMobile || isCompactDesktop) && !focusMode && (
                <button
                  className="btn-secondary"
                  onClick={() => setShowCompactHistory((prev) => !prev)}
                >
                  {showCompactHistory ? 'HIDE HISTORY' : 'HISTORY'}
                </button>
              )}
              {isAgentMode && isLoading && agentRunID && (
                <button className="btn-secondary" onClick={handleCancel}>
                  CANCEL RUN
                </button>
              )}
              <button
                className="btn-secondary"
                onClick={() => setFocusMode((prev) => !prev)}
                title={focusMode ? 'Exit focus mode (Esc)' : 'Enter focus mode'}
              >
                {focusMode ? 'EXIT' : 'FOCUS'}
              </button>
              <button className="btn-secondary" onClick={handleClear}>CLEAR</button>
              <button className="btn-primary" onClick={handleRun} disabled={isLoading || !canRun}>
                {isLoading ? (isAgentMode ? 'RUNNING AGENT...' : 'GENERATING...') : isMobile ? 'RUN' : (isAgentMode ? 'RUN AGENT' : 'RUN INFERENCE')}
              </button>
            </div>
          </div>

          {(isMobile || isCompactDesktop) && !focusMode && showMobileSettings && (
            <section style={{
              padding: '1rem',
              borderBottom: 'var(--grid-line)',
              backgroundColor: 'var(--bg-accent)',
            }}>
              <CollapsibleSection
                title="SETTINGS"
                description={isAgentMode ? 'Mode selection, agent controls, and workspace-safe tools.' : 'Model selection, decoding controls, and system prompt.'}
                defaultExpanded
              >
                {settingsControls}
              </CollapsibleSection>
            </section>
          )}

          {(isMobile || isCompactDesktop) && !focusMode && showCompactHistory && (
            <section style={{
              padding: '1rem',
              borderBottom: 'var(--grid-line)',
              backgroundColor: 'rgba(244, 242, 238, 0.72)',
            }}>
              <CollapsibleSection
                title="REQUEST HISTORY"
                description="Recent prompts, latency, and execution results."
                defaultExpanded
              >
                {historyPanel}
              </CollapsibleSection>
            </section>
          )}

          <section style={{
            padding: isMobile ? '1rem' : '1.25rem 1.5rem', borderBottom: 'var(--grid-line)',
            display: 'flex', flexDirection: 'column',
            height: 132, flexShrink: 0,
          }}>
            <label className="label-text" style={{ marginBottom: '0.75rem', display: 'block' }}>
              {isAgentMode ? 'TASK' : 'USER PROMPT'}
            </label>
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder={isAgentMode ? 'Ask Hermes to inspect models, workers, deployments, instances, or provider health...' : 'Type your instruction here...'}
              onKeyDown={(e) => { if (e.key === 'Enter' && e.metaKey) handleRun(); }}
              style={{
                width: '100%', flex: 1, border: 'none',
                background: 'transparent', resize: 'none', outline: 'none',
                fontFamily: 'var(--font-main)', fontSize: isMobile ? '0.95rem' : '1.05rem', lineHeight: 1.6,
                color: 'var(--text-primary)',
              }}
            />
          </section>

          <section style={{
            flex: 1, display: 'flex', flexDirection: 'column',
            overflow: 'hidden', minHeight: 0,
          }}>
            <div style={{
              padding: isMobile ? '0.75rem 1rem 0.5rem' : '0.85rem 1.5rem 0.4rem', flexShrink: 0,
              display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              flexWrap: 'wrap', gap: '0.5rem',
            }}>
              <label className="label-text">{isAgentMode ? 'AGENT OUTPUT' : 'OUTPUT'}</label>
              {tokenUsage && !isAgentMode && (
                <div className="mono" style={{
                  fontSize: '0.65rem', color: 'var(--text-secondary)',
                  display: 'flex', gap: '1rem', flexWrap: 'wrap',
                }}>
                  <span>{tokenUsage.promptTokens} prompt</span>
                  <span>{tokenUsage.completionTokens} completion</span>
                  <span>{tokenUsage.totalTokens} total</span>
                  <span>{tokenUsage.tokensPerSec.toFixed(1)} tok/s</span>
                </div>
              )}
              {isAgentMode && agentDetail && (
                <div className="mono" style={{
                  fontSize: '0.65rem', color: 'var(--text-secondary)',
                  display: 'flex', gap: '1rem', flexWrap: 'wrap',
                }}>
                  <span>{formatAgentStatus(agentDetail.run.status)}</span>
                  <span>{agentDetail.run.current_step}/{agentDetail.run.max_steps} steps</span>
                  <span>{agentDetail.steps.length} events</span>
                </div>
              )}
            </div>
            <div ref={responseRef} style={{
              flex: 1, overflowY: 'auto', padding: isMobile ? '0.5rem 1rem 1rem' : '0.45rem 1.5rem 1.5rem',
              minHeight: 0,
            }}>
              {isAgentMode && agentDetail && (
                <div style={{
                  marginBottom: '1rem',
                  padding: '1rem',
                  background: 'rgba(244, 242, 238, 0.72)',
                  border: '1px solid var(--border-color)',
                }}>
                  <div className="label-text" style={{ marginBottom: '0.6rem' }}>RUN STATUS</div>
                  <div className="mono" style={{ fontSize: '0.72rem', display: 'flex', gap: '1rem', flexWrap: 'wrap', marginBottom: agentDetail.run.failure_reason ? '0.75rem' : 0 }}>
                    <span>{formatAgentStatus(agentDetail.run.status)}</span>
                    <span>{agentDetail.run.agent_id}</span>
                    <span>{agentDetail.run.model.split('/').pop()}</span>
                    {agentRunID && <span>{agentRunID}</span>}
                  </div>
                  {agentDetail.run.failure_reason && (
                    <div style={{ fontSize: '0.88rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                      {agentDetail.run.failure_reason}
                    </div>
                  )}
                </div>
              )}

              {response ? (
                <div className="markdown-output">
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    rehypePlugins={[rehypeHighlight]}
                    components={{
                      pre({ children, ...props }) {
                        return (
                          <pre {...props} style={{
                            background: '#F4F2EE',
                            border: '1px solid var(--border-color)',
                            padding: '1.25rem',
                            overflow: 'auto',
                            fontSize: '0.85rem',
                            lineHeight: 1.6,
                            marginBottom: '1rem',
                          }}>
                            {children}
                          </pre>
                        );
                      },
                      code({ className, children, ...props }) {
                        const isInline = !className;
                        if (isInline) {
                          return (
                            <code {...props} style={{
                              background: '#F4F2EE',
                              padding: '0.15rem 0.4rem',
                              fontSize: '0.88em',
                              fontFamily: 'var(--font-mono)',
                              border: '1px solid var(--border-color)',
                            }}>
                              {children}
                            </code>
                          );
                        }
                        return <code className={className} {...props} style={{ fontFamily: 'var(--font-mono)' }}>{children}</code>;
                      },
                    }}
                  >
                    {response}
                  </ReactMarkdown>
                </div>
              ) : (
                <span style={{ color: 'var(--text-secondary)', opacity: 0.4, fontSize: '1.05rem' }}>
                  {isAgentMode ? 'Hermes output and run trace will appear here...' : 'Response will appear here...'}
                </span>
              )}

              {isAgentMode && agentDetail?.steps.length ? (
                <div style={{ marginTop: '1.5rem' }}>
                  <label className="label-text" style={{ marginBottom: '0.75rem', display: 'block' }}>RUN TRACE</label>
                  <div style={{ display: 'grid', gap: '0.9rem' }}>
                    {agentDetail.steps.map((step) => (
                      <div key={step.id} style={{
                        border: '1px solid var(--border-color)',
                        background: 'rgba(244, 242, 238, 0.4)',
                        padding: '0.9rem 1rem',
                      }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap', marginBottom: '0.75rem' }}>
                          <div className="mono" style={{ fontSize: '0.72rem' }}>
                            {String(step.index + 1).padStart(2, '0')} · {formatStepType(step.type)}{step.tool_name ? ` · ${step.tool_name}` : ''}
                          </div>
                          <div className="mono" style={{ fontSize: '0.68rem', color: 'var(--text-secondary)' }}>
                            {new Date(step.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                          </div>
                        </div>
                        <pre style={{
                          background: '#F4F2EE',
                          border: '1px solid var(--border-color)',
                          padding: '0.9rem',
                          overflow: 'auto',
                          fontSize: '0.78rem',
                          lineHeight: 1.55,
                          margin: 0,
                          whiteSpace: 'pre-wrap',
                          wordBreak: 'break-word',
                        }}>
                          {formatStepPayload(step.payload)}
                        </pre>
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </section>
        </main>

        {showDesktopHistoryRail && <aside style={{
          borderLeft: 'var(--grid-line)',
          backgroundColor: 'var(--bg-accent)',
          padding: '1.5rem',
          overflowY: 'auto',
        }}>
          <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>REQUEST HISTORY</label>
          {historyPanel}
        </aside>}
      </div>
    </div>
  );
}
