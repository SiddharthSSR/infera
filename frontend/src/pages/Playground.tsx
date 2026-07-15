import { useEffect, useMemo } from 'react';
import { CollapsibleSection } from '../components/CollapsibleSection';
import { LabelText, ActionButton } from '../components/shared';
import { PlaygroundSkeleton } from '../components/skeletons';
import { useAgents, useModels } from '../hooks/useRuntimeApi';
import { PlaygroundHistoryPanel } from '../components/playground/PlaygroundHistoryPanel';
import { PlaygroundOutputPanel } from '../components/playground/PlaygroundOutputPanel';
import { PlaygroundSettingsPanel } from '../components/playground/PlaygroundSettingsPanel';
import { useChat } from '../lib/chat-context';
import { usePlaygroundExecutionState } from '../hooks/usePlaygroundExecutionState';
import { usePlaygroundLayoutState } from '../hooks/usePlaygroundLayoutState';
import { formatAgentStatus } from '../lib/labels';

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
  const { data: models, isLoading: modelsLoading } = useModels();
  const { data: agentsData, error: agentsError } = useAgents();
  const allModels = useMemo(() => models ?? [], [models]);
  const agents = useMemo(() => agentsData?.agents ?? [], [agentsData?.agents]);

  const isAgentMode = playgroundMode === 'agent';
  const agentModeAvailable = agents.length > 0;
  const activeAgent =
    agents.find((agent) => agent.id === selectedAgentID) ||
    agents.find((agent) => agent.id === agentsData?.default_agent_id) ||
    agents[0] ||
    null;

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
  const {
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
  } = usePlaygroundExecutionState({
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
  });
  const canExecute = Boolean(canRun && (!isAgentMode || agentModeAvailable));
  const {
    focusMode,
    isCompactDesktop,
    isExtraSmall,
    isMobile,
    isTablet,
    playgroundGridTemplateColumns,
    promptHeight,
    setFocusMode,
    showDesktopHistoryRail,
    showDesktopSettingsRail,
  } = usePlaygroundLayoutState(history.length);

  const runStatusText = isAgentMode
    ? (agentDetail?.run.status
      ? `agent ${formatAgentStatus(agentDetail.run.status).toLowerCase()}`
      : (isLoading ? 'starting agent run...' : (activeAgent ? `${activeAgent.name.toLowerCase()} ready` : 'agent mode unavailable')))
    : (isLoading ? 'generating...' : 'ready to inference');

  // Tablet (768–1024): settings/history always visible, stacked around editor
  const tabletSettingsSection = isTablet && !focusMode && (
    <section
      style={{
        padding: '1rem',
        borderBottom: 'var(--grid-line)',
        backgroundColor: 'var(--bg-accent)',
        overflowY: 'auto',
        maxHeight: '35vh',
      }}
    >
      <PlaygroundSettingsPanel
        playgroundMode={playgroundMode}
        onModeChange={handleModeChange}
        agentModeAvailable={agentModeAvailable}
        selectedModel={selectedModel}
        onSelectedModelChange={setSelectedModel}
        models={allModels}
        selectedAgentID={selectedAgentID}
        onSelectedAgentChange={(agentID) => {
          const nextAgent = agents.find((agent) => agent.id === agentID);
          setSelectedAgentID(agentID);
          if (nextAgent) {
            setAgentMaxSteps(nextAgent.default_max_steps);
          }
        }}
        agents={agents}
        activeAgent={activeAgent}
        agentsErrorMessage={agentsError instanceof Error ? agentsError.message : null}
        agentExecutionMode={agentExecutionMode}
        onAgentExecutionModeChange={setAgentExecutionMode}
        agentAnalysisDepth={agentAnalysisDepth}
        onAgentAnalysisDepthChange={setAgentAnalysisDepth}
        agentMaxSteps={agentMaxSteps}
        onAgentMaxStepsChange={setAgentMaxSteps}
        fileInputRef={fileInputRef}
        screenshotFile={screenshotFile}
        screenshotPreviewURL={screenshotPreviewURL}
        onFileSelection={handleFileSelection}
        temperature={temperature}
        onTemperatureChange={setTemperature}
        maxTokens={maxTokens}
        onMaxTokensChange={setMaxTokens}
        topP={topP}
        onTopPChange={setTopP}
        freqPenalty={freqPenalty}
        onFreqPenaltyChange={setFreqPenalty}
        systemPrompt={systemPrompt}
        onSystemPromptChange={setSystemPrompt}
      />
    </section>
  );

  const tabletHistorySection = isTablet && !focusMode && history.length > 0 && (
    <section
      style={{
        padding: '1rem',
        borderTop: 'var(--grid-line)',
        backgroundColor: 'rgba(244, 242, 238, 0.72)',
        overflowY: 'auto',
        maxHeight: '30vh',
      }}
      >
        <LabelText as="div" style={{ marginBottom: '0.75rem' }}>REQUEST HISTORY</LabelText>
        <PlaygroundHistoryPanel history={history} onClearHistory={() => setHistory([])} />
      </section>
  );

  // Mobile (≤768): accordion sections below the editor
  const mobileAccordionPanels = isMobile && !focusMode && (
    <div style={{ borderTop: 'var(--grid-line)' }}>
      <section
        style={{
          backgroundColor: 'var(--bg-accent)',
          borderBottom: 'var(--grid-line)',
        }}
      >
        <CollapsibleSection
          title="MODEL & PARAMETERS"
          description={isAgentMode ? 'Mode, agent, screenshots, and tools.' : 'Model, decoding, and system prompt.'}
        >
          <div style={{ padding: '0 0.75rem 0.75rem' }}>
            <PlaygroundSettingsPanel
              playgroundMode={playgroundMode}
              onModeChange={handleModeChange}
              agentModeAvailable={agentModeAvailable}
              selectedModel={selectedModel}
              onSelectedModelChange={setSelectedModel}
              models={allModels}
              selectedAgentID={selectedAgentID}
              onSelectedAgentChange={(agentID) => {
                const nextAgent = agents.find((agent) => agent.id === agentID);
                setSelectedAgentID(agentID);
                if (nextAgent) {
                  setAgentMaxSteps(nextAgent.default_max_steps);
                }
              }}
              agents={agents}
              activeAgent={activeAgent}
              agentsErrorMessage={agentsError instanceof Error ? agentsError.message : null}
              agentExecutionMode={agentExecutionMode}
              onAgentExecutionModeChange={setAgentExecutionMode}
              agentAnalysisDepth={agentAnalysisDepth}
              onAgentAnalysisDepthChange={setAgentAnalysisDepth}
              agentMaxSteps={agentMaxSteps}
              onAgentMaxStepsChange={setAgentMaxSteps}
              fileInputRef={fileInputRef}
              screenshotFile={screenshotFile}
              screenshotPreviewURL={screenshotPreviewURL}
              onFileSelection={handleFileSelection}
              temperature={temperature}
              onTemperatureChange={setTemperature}
              maxTokens={maxTokens}
              onMaxTokensChange={setMaxTokens}
              topP={topP}
              onTopPChange={setTopP}
              freqPenalty={freqPenalty}
              onFreqPenaltyChange={setFreqPenalty}
              systemPrompt={systemPrompt}
              onSystemPromptChange={setSystemPrompt}
            />
          </div>
        </CollapsibleSection>
      </section>
      <section
        style={{
          backgroundColor: 'rgba(244, 242, 238, 0.72)',
        }}
      >
        <CollapsibleSection
          title="REQUEST HISTORY"
          description="Recent prompts, latency, and execution results."
        >
          <div style={{ padding: '0 0.75rem 0.75rem' }}>
            <PlaygroundHistoryPanel history={history} onClearHistory={() => setHistory([])} />
          </div>
        </CollapsibleSection>
      </section>
    </div>
  );


  if (modelsLoading) return <PlaygroundSkeleton />;

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
        <header
          className="display-text"
          style={{
            fontSize: isExtraSmall ? '2.4rem' : isMobile ? '3rem' : '4.2rem',
            padding: isMobile ? '1rem 0 0.75rem' : '1.5rem 0 1.2rem',
          }}
        >
          PLAYGROUND
        </header>
      )}

      <div
        style={{
          display: (isTablet || isMobile) ? 'flex' : 'grid',
          flexDirection: (isTablet || isMobile) ? 'column' : undefined,
          gridTemplateColumns: (isTablet || isMobile) ? undefined : playgroundGridTemplateColumns,
          flexGrow: 1,
          overflow: (isTablet || isMobile) ? 'auto' : 'hidden',
          height: focusMode
            ? '100dvh'
            : isMobile
              ? 'auto'
              : isTablet
                ? 'auto'
                : 'calc(100dvh - 190px)',
          minHeight: focusMode
            ? undefined
            : (isMobile || isTablet)
              ? 'calc(100dvh - 150px)'
              : undefined,
        }}
      >
        {/* Tablet: settings stacked above editor */}
        {tabletSettingsSection}

        {showDesktopSettingsRail && (
          <aside
            style={{
              padding: '1.5rem',
              borderRight: 'var(--grid-line)',
              overflowY: 'auto',
            }}
          >
            <PlaygroundSettingsPanel
              playgroundMode={playgroundMode}
              onModeChange={handleModeChange}
              agentModeAvailable={agentModeAvailable}
              selectedModel={selectedModel}
              onSelectedModelChange={setSelectedModel}
              models={allModels}
              selectedAgentID={selectedAgentID}
              onSelectedAgentChange={(agentID) => {
                const nextAgent = agents.find((agent) => agent.id === agentID);
                setSelectedAgentID(agentID);
                if (nextAgent) {
                  setAgentMaxSteps(nextAgent.default_max_steps);
                }
              }}
              agents={agents}
              activeAgent={activeAgent}
              agentsErrorMessage={agentsError instanceof Error ? agentsError.message : null}
              agentExecutionMode={agentExecutionMode}
              onAgentExecutionModeChange={setAgentExecutionMode}
              agentAnalysisDepth={agentAnalysisDepth}
              onAgentAnalysisDepthChange={setAgentAnalysisDepth}
              agentMaxSteps={agentMaxSteps}
              onAgentMaxStepsChange={setAgentMaxSteps}
              fileInputRef={fileInputRef}
              screenshotFile={screenshotFile}
              screenshotPreviewURL={screenshotPreviewURL}
              onFileSelection={handleFileSelection}
              temperature={temperature}
              onTemperatureChange={setTemperature}
              maxTokens={maxTokens}
              onMaxTokensChange={setMaxTokens}
              topP={topP}
              onTopPChange={setTopP}
              freqPenalty={freqPenalty}
              onFreqPenaltyChange={setFreqPenalty}
              systemPrompt={systemPrompt}
              onSystemPromptChange={setSystemPrompt}
            />
          </aside>
        )}

        <main style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', minHeight: 0, flex: (isTablet || isMobile) ? '1 1 auto' : undefined }}>
          {/* Toolbar */}
          <div
            style={{
              padding: isExtraSmall ? '0.65rem 0.75rem' : isMobile ? '0.75rem 1rem' : '1rem 1.5rem',
              borderBottom: 'var(--grid-line)',
              display: 'flex',
              flexDirection: isMobile ? 'column' : 'row',
              justifyContent: 'space-between',
              alignItems: isMobile ? 'stretch' : 'center',
              gap: isMobile ? '0.5rem' : '0.75rem',
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

            {isMobile ? (
              <div style={{ display: 'grid', gap: '0.4rem' }}>
                <div style={{ display: 'flex', gap: '0.5rem' }}>
                  <ActionButton
                    variant="primary"
                    style={{ flex: 1, minHeight: 44 }}
                    onClick={handleRun}
                    disabled={isLoading || !canExecute}
                  >
                    {isLoading ? (isAgentMode ? 'RUNNING...' : 'GENERATING...') : isAgentMode ? 'RUN AGENT' : 'RUN'}
                  </ActionButton>
                  {isAgentMode && isLoading && agentRunID && (
                    <button className="btn-secondary" style={{ minHeight: 44 }} onClick={handleCancel}>
                      CANCEL
                    </button>
                  )}
                  <button className="btn-secondary" style={{ minHeight: 44 }} onClick={handleClear}>CLEAR</button>
                </div>
                <div style={{ display: 'flex', gap: '0.5rem' }}>
                  <button className="btn-secondary" style={{ flex: 1, minHeight: 40 }} onClick={() => setFocusMode((value) => !value)}>
                    {focusMode ? 'EXIT' : 'FOCUS'}
                  </button>
                </div>
              </div>
            ) : (
              <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                {isAgentMode && isLoading && agentRunID && (
                  <button className="btn-secondary" onClick={handleCancel}>
                    CANCEL RUN
                  </button>
                )}
                <button className="btn-secondary" onClick={() => setFocusMode((value) => !value)}>
                  {focusMode ? 'EXIT' : 'FOCUS'}
                </button>
                <button className="btn-secondary" onClick={handleClear}>CLEAR</button>
                <ActionButton variant="primary" onClick={handleRun} disabled={isLoading || !canExecute}>
                  {isLoading ? (isAgentMode ? 'RUNNING AGENT...' : 'GENERATING...') : isAgentMode ? 'RUN AGENT' : 'RUN INFERENCE'}
                </ActionButton>
              </div>
            )}
          </div>

          {/* Prompt input */}
          <section
            style={{
              padding: isExtraSmall ? '0.65rem 0.75rem' : isMobile ? '0.75rem 1rem' : '1.25rem 1.5rem',
              borderBottom: 'var(--grid-line)',
              display: 'flex',
              flexDirection: 'column',
              height: promptHeight,
              flexShrink: 0,
            }}
          >
            <LabelText as="label" style={{ marginBottom: '0.5rem', display: 'block' }}>
              {isAgentMode ? 'TASK' : 'USER PROMPT'}
            </LabelText>
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
                fontSize: isExtraSmall ? '0.9rem' : isMobile ? '0.95rem' : '1.05rem',
                lineHeight: 1.6,
                color: 'var(--text-primary)',
              }}
            />
          </section>

          {/* Output area */}
          <PlaygroundOutputPanel
            isAgentMode={isAgentMode}
            isExtraSmall={isExtraSmall}
            isMobile={isMobile}
            isLoading={isLoading}
            responseRef={responseRef}
            response={response}
            tokenUsage={tokenUsage}
            agentDetail={agentDetail}
            agentExecutionMode={agentExecutionMode}
            agentAnalysisDepth={agentAnalysisDepth}
          />

        </main>

        {/* Mobile: accordion panels below editor */}
        {mobileAccordionPanels}

        {/* Tablet: history stacked below editor */}
        {tabletHistorySection}

        {showDesktopHistoryRail && (
          <aside
            style={{
              padding: '1.5rem',
              borderLeft: 'var(--grid-line)',
              overflowY: 'auto',
            }}
          >
            <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>REQUEST HISTORY</LabelText>
            <PlaygroundHistoryPanel history={history} onClearHistory={() => setHistory([])} />
          </aside>
        )}
      </div>
    </div>
  );
}
