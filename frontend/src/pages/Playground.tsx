import { useState, useRef, useCallback, useEffect } from 'react';
import { toast } from 'sonner';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import { useModels } from '../hooks/useApi';
import { CollapsibleSection } from '../components/CollapsibleSection';
import { useChat } from '../lib/chat-context';
import { streamChatCompletion } from '../lib/api';

interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  tokensPerSec: number;
}

export function Playground() {
  const {
    history,
    setHistory,
    selectedModel,
    setSelectedModel,
    temperature,
    setTemperature,
    maxTokens,
    setMaxTokens,
  } = useChat();
  const { data: models } = useModels();
  const allModels = models || [];

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
  const responseRef = useRef<HTMLDivElement>(null);

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

  // Auto-select the first available model after data loads.
  useEffect(() => {
    if (!selectedModel && allModels.length > 0) {
      setSelectedModel(allModels[0].id);
    }
  }, [allModels, selectedModel, setSelectedModel]);

  // Auto-scroll response area as content streams in
  useEffect(() => {
    if (responseRef.current && isLoading) {
      responseRef.current.scrollTop = responseRef.current.scrollHeight;
    }
  }, [response, isLoading]);

  const handleRun = useCallback(async () => {
    if (!prompt.trim() || !selectedModel) return;
    setIsLoading(true);
    setResponse('');
    setTokenUsage(null);
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

      setHistory(prev => [{
        id: Math.random().toString(36).slice(2),
        time: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
        latencyMs: latency,
        preview: prompt.slice(0, 50) + (prompt.length > 50 ? '...' : ''),
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
  }, [prompt, selectedModel, systemPrompt, temperature, maxTokens, topP, freqPenalty, setHistory]);

  const handleClear = () => {
    setPrompt('');
    setResponse('');
    setTokenUsage(null);
  };

  const settingsControls = (
    <>
      <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>ACTIVE MODEL</label>
      <select
        value={selectedModel}
        onChange={e => setSelectedModel(e.target.value)}
        style={{
          width: '100%', padding: '0.75rem 0', background: 'transparent',
          border: 'none', borderBottom: '1px solid var(--text-primary)',
          fontFamily: 'var(--font-main)', fontSize: '1rem', outline: 'none',
          marginBottom: '2rem', cursor: 'pointer', color: 'var(--text-primary)',
        }}
      >
        {allModels.length === 0 && <option value="">No models available</option>}
        {allModels.map(m => (
          <option key={m.id} value={m.id}>
            {m.id.split('/').pop()}{m.loaded === false ? ' (not loaded)' : ''}{m.parameters ? ` — ${m.parameters}` : ''}
          </option>
        ))}
      </select>

      <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>PARAMETERS</label>

      <div style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem' }}>Temperature</span>
          <span className="mono" style={{ fontSize: '0.85rem' }}>{temperature.toFixed(2)}</span>
        </div>
        <input type="range" min="0" max="2" step="0.01" value={temperature}
          onChange={e => setTemperature(parseFloat(e.target.value))} />
      </div>

      <div style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem' }}>Max Tokens</span>
          <span className="mono" style={{ fontSize: '0.85rem' }}>{maxTokens}</span>
        </div>
        <input type="range" min="1" max="8192" step="1" value={maxTokens}
          onChange={e => setMaxTokens(parseInt(e.target.value))} />
      </div>

      <div style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem' }}>Top P</span>
          <span className="mono" style={{ fontSize: '0.85rem' }}>{topP.toFixed(2)}</span>
        </div>
        <input type="range" min="0" max="1" step="0.01" value={topP}
          onChange={e => setTopP(parseFloat(e.target.value))} />
      </div>

      <div style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
          <span style={{ fontSize: '0.85rem' }}>Frequency Penalty</span>
          <span className="mono" style={{ fontSize: '0.85rem' }}>{freqPenalty.toFixed(2)}</span>
        </div>
        <input type="range" min="0" max="2" step="0.01" value={freqPenalty}
          onChange={e => setFreqPenalty(parseFloat(e.target.value))} />
      </div>

      <div style={{ marginTop: '2rem' }}>
        <label className="label-text" style={{ marginBottom: '1rem', display: 'block' }}>SYSTEM PROMPT</label>
        <textarea
          value={systemPrompt}
          onChange={e => setSystemPrompt(e.target.value)}
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
  );

  const historyPanel = (
    <>
      {history.length === 0 ? (
        <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', padding: '1rem 0' }}>
          No requests yet. Run an inference to see history.
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
              // Could restore the prompt from history
            }}
          >
            <span className="mono" style={{ fontSize: '0.65rem', color: 'var(--text-secondary)', display: 'block', marginBottom: '0.25rem' }}>
              {entry.time} - {entry.latencyMs}ms
            </span>
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

  return (
    <div style={focusMode ? {
      position: 'fixed', inset: 0, zIndex: 100,
      background: 'var(--bg-paper)',
      display: 'flex', flexDirection: 'column',
    } : {}}>
      {/* Playground has its own display header */}
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
        {/* Left Sidebar - Parameters */}
        {showDesktopSettingsRail && <aside style={{
          padding: '1.5rem',
          borderRight: 'var(--grid-line)',
          overflowY: 'auto',
        }}>
          {settingsControls}
        </aside>}

        {/* Center - Editor */}
        <main style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {/* Action Bar */}
          <div style={{
            padding: isMobile ? '1rem' : '1rem 1.5rem', borderBottom: 'var(--grid-line)',
            display: 'flex', justifyContent: 'space-between', alignItems: 'center',
            flexWrap: 'wrap', gap: '0.75rem',
            flexShrink: 0,
          }}>
            <div style={{ display: 'grid', gap: '0.35rem' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: '0.65rem', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-secondary)' }}>
                <div style={{ width: 6, height: 6, background: isLoading ? 'var(--color-warning)' : 'var(--color-success)', borderRadius: '50%' }} />
                {isLoading ? 'generating...' : 'ready to inference'}
              </div>
              {!showDesktopHistoryRail && !isMobile && !isCompactDesktop && (
                <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  Request history will appear here after the first successful run.
                </div>
              )}
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
              {(isMobile || isCompactDesktop) && !focusMode && (
                <button
                  className="btn-secondary"
                  onClick={() => setShowMobileSettings(prev => !prev)}
                >
                  {showMobileSettings ? 'HIDE SETTINGS' : 'SETTINGS'}
                </button>
              )}
              {(isMobile || isCompactDesktop) && !focusMode && (
                <button
                  className="btn-secondary"
                  onClick={() => setShowCompactHistory(prev => !prev)}
                >
                  {showCompactHistory ? 'HIDE HISTORY' : 'HISTORY'}
                </button>
              )}
              <button
                className="btn-secondary"
                onClick={() => setFocusMode(prev => !prev)}
                title={focusMode ? 'Exit focus mode (Esc)' : 'Enter focus mode'}
              >
                {focusMode ? 'EXIT' : 'FOCUS'}
              </button>
              <button className="btn-secondary" onClick={handleClear}>CLEAR</button>
              <button className="btn-primary" onClick={handleRun} disabled={isLoading || !prompt.trim()}>
                {isLoading ? 'GENERATING...' : isMobile ? 'RUN' : 'RUN INFERENCE'}
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
                description="Model selection, decoding controls, and system prompt."
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
                description="Recent prompts, latency, and token counts."
                defaultExpanded
              >
                {historyPanel}
              </CollapsibleSection>
            </section>
          )}

          {/* Prompt Area - fixed height, does not grow */}
          <section style={{
            padding: isMobile ? '1rem' : '1.25rem 1.5rem', borderBottom: 'var(--grid-line)',
            display: 'flex', flexDirection: 'column',
            height: 132, flexShrink: 0,
          }}>
            <label className="label-text" style={{ marginBottom: '0.75rem', display: 'block' }}>USER PROMPT</label>
            <textarea
              value={prompt}
              onChange={e => setPrompt(e.target.value)}
              placeholder="Type your instruction here..."
              onKeyDown={e => { if (e.key === 'Enter' && e.metaKey) handleRun(); }}
              style={{
                width: '100%', flex: 1, border: 'none',
                background: 'transparent', resize: 'none', outline: 'none',
                fontFamily: 'var(--font-main)', fontSize: isMobile ? '0.95rem' : '1.05rem', lineHeight: 1.6,
                color: 'var(--text-primary)',
              }}
            />
          </section>

          {/* Response Area - takes remaining space, scrolls independently */}
          <section style={{
            flex: 1, display: 'flex', flexDirection: 'column',
            overflow: 'hidden', minHeight: 0,
          }}>
            <div style={{
              padding: isMobile ? '0.75rem 1rem 0.5rem' : '0.85rem 1.5rem 0.4rem', flexShrink: 0,
              display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              flexWrap: 'wrap', gap: '0.5rem',
            }}>
              <label className="label-text">OUTPUT</label>
              {tokenUsage && (
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
            </div>
            <div ref={responseRef} style={{
              flex: 1, overflowY: 'auto', padding: isMobile ? '0.5rem 1rem 1rem' : '0.45rem 1.5rem 1.5rem',
              minHeight: 0,
            }}>
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
                  Response will appear here...
                </span>
              )}
            </div>
          </section>
        </main>

        {/* Right Sidebar - History */}
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
