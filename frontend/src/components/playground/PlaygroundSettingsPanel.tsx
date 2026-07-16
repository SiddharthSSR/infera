import type { RefObject } from 'react';
import { LabelText } from '../shared';
import type {
  AgentAnalysisDepth,
  AgentDescriptor,
  AgentExecutionMode,
  Model,
  PlaygroundMode,
} from '../../types';

const agentModeOptions: Array<{
  value: AgentExecutionMode;
  label: string;
  eyebrow: string;
  description: string;
}> = [
  {
    value: 'operations',
    label: 'OPERATIONS',
    eyebrow: 'DEFAULT',
    description: 'Workspace health, deployments, provider signals, and quota-aware checks.',
  },
  {
    value: 'research',
    label: 'RESEARCH',
    eyebrow: 'CITED',
    description: 'Official docs, status pages, and release notes with evidence-backed answers.',
  },
  {
    value: 'multimodal',
    label: 'MULTIMODAL',
    eyebrow: 'SCREENSHOT',
    description: 'Image-backed investigation for uploaded workspace screenshots and console captures.',
  },
];

function toolAvailableInMode(modes: AgentExecutionMode[] | undefined, mode: AgentExecutionMode) {
  if (!modes || modes.length === 0) {
    return true;
  }
  return modes.includes(mode);
}

interface PlaygroundSettingsPanelProps {
  playgroundMode: PlaygroundMode;
  onModeChange: (mode: PlaygroundMode) => void;
  agentModeAvailable: boolean;
  selectedModel: string;
  onSelectedModelChange: (modelID: string) => void;
  models: Model[];
  selectedAgentID: string;
  onSelectedAgentChange: (agentID: string) => void;
  agents: AgentDescriptor[];
  activeAgent: AgentDescriptor | null;
  agentsErrorMessage: string | null;
  agentExecutionMode: AgentExecutionMode;
  onAgentExecutionModeChange: (mode: AgentExecutionMode) => void;
  agentAnalysisDepth: AgentAnalysisDepth;
  onAgentAnalysisDepthChange: (depth: AgentAnalysisDepth) => void;
  agentMaxSteps: number;
  onAgentMaxStepsChange: (steps: number) => void;
  fileInputRef: RefObject<HTMLInputElement>;
  screenshotFile: File | null;
  screenshotPreviewURL: string;
  onFileSelection: (file: File | null) => void;
  temperature: number;
  onTemperatureChange: (value: number) => void;
  maxTokens: number;
  onMaxTokensChange: (value: number) => void;
  topP: number;
  onTopPChange: (value: number) => void;
  freqPenalty: number;
  onFreqPenaltyChange: (value: number) => void;
  systemPrompt: string;
  onSystemPromptChange: (value: string) => void;
}

export function PlaygroundSettingsPanel({
  playgroundMode,
  onModeChange,
  agentModeAvailable,
  selectedModel,
  onSelectedModelChange,
  models,
  selectedAgentID,
  onSelectedAgentChange,
  agents,
  activeAgent,
  agentsErrorMessage,
  agentExecutionMode,
  onAgentExecutionModeChange,
  agentAnalysisDepth,
  onAgentAnalysisDepthChange,
  agentMaxSteps,
  onAgentMaxStepsChange,
  fileInputRef,
  screenshotFile,
  screenshotPreviewURL,
  onFileSelection,
  temperature,
  onTemperatureChange,
  maxTokens,
  onMaxTokensChange,
  topP,
  onTopPChange,
  freqPenalty,
  onFreqPenaltyChange,
  systemPrompt,
  onSystemPromptChange,
}: PlaygroundSettingsPanelProps) {
  const isAgentMode = playgroundMode === 'agent';
  const toolList = activeAgent?.tools || [];
  const visibleToolList = toolList.filter((tool) => toolAvailableInMode(tool.modes, agentExecutionMode));

  return (
    <>
      <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>PLAY MODE</LabelText>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem', marginBottom: '2rem' }}>
        <button
          className={playgroundMode === 'chat' ? 'btn-primary' : 'btn-secondary'}
          type="button"
          aria-pressed={playgroundMode === 'chat'}
          onClick={() => onModeChange('chat')}
        >
          CHAT
        </button>
        <button
          className={playgroundMode === 'agent' ? 'btn-primary' : 'btn-secondary'}
          type="button"
          aria-pressed={playgroundMode === 'agent'}
          onClick={() => onModeChange('agent')}
          disabled={!agentModeAvailable}
          title={!agentModeAvailable ? 'Agents are unavailable on this gateway' : 'Run a backend agent'}
        >
          AGENT
        </button>
      </div>

      <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>ACTIVE MODEL</LabelText>
      <select
        value={selectedModel}
        onChange={(event) => onSelectedModelChange(event.target.value)}
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
        {models.length === 0 && <option value="">No models available</option>}
        {models.map((model) => (
          <option key={model.id} value={model.id}>
            {model.id.split('/').pop()}
            {model.loaded === false ? ' (not loaded)' : ''}
            {model.parameters ? ` — ${model.parameters}` : ''}
          </option>
        ))}
      </select>

      {isAgentMode ? (
        <>
          <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>ACTIVE AGENT</LabelText>
          <select
            value={selectedAgentID}
            onChange={(event) => onSelectedAgentChange(event.target.value)}
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
            {activeAgent?.description || agentsErrorMessage || 'Agents are unavailable on this gateway.'}
          </div>

          <LabelText as="label" style={{ marginBottom: '0.75rem', display: 'block' }}>AGENT MODE</LabelText>
          <div style={{ display: 'grid', gap: '0.7rem', marginBottom: '1rem' }}>
            {agentModeOptions.map((mode) => {
              const isActive = agentExecutionMode === mode.value;
              return (
                <button
                  key={mode.value}
                  type="button"
                  aria-label={mode.label}
                  aria-pressed={isActive}
                  onClick={() => onAgentExecutionModeChange(mode.value)}
                  style={{
                    width: '100%',
                    textAlign: 'left',
                    border: isActive ? '1px solid #161B2C' : '1px solid rgba(22, 27, 44, 0.12)',
                    background: isActive
                      ? 'linear-gradient(135deg, #161B2C 0%, #232C46 100%)'
                      : 'linear-gradient(135deg, rgba(255,255,255,0.94) 0%, rgba(244,242,238,0.88) 100%)',
                    color: isActive ? '#F7F5F1' : 'var(--text-primary)',
                    padding: '0.9rem 0.95rem',
                    display: 'grid',
                    gap: '0.35rem',
                    cursor: 'pointer',
                    boxShadow: isActive ? '0 10px 24px rgba(22, 27, 44, 0.14)' : 'none',
                    transition: 'background 180ms ease, box-shadow 180ms ease, border-color 180ms ease',
                  }}
                >
                  <div
                    className="mono"
                    style={{
                      fontSize: '0.62rem',
                      letterSpacing: '0.08em',
                      color: isActive ? 'rgba(247, 245, 241, 0.68)' : 'var(--text-secondary)',
                    }}
                  >
                    {mode.eyebrow}
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem' }}>
                    <span style={{ fontSize: '0.95rem', fontWeight: 600, letterSpacing: '0.01em' }}>{mode.label}</span>
                    <span
                      className="mono"
                      style={{
                        fontSize: '0.58rem',
                        padding: '0.18rem 0.38rem',
                        border: isActive ? '1px solid rgba(247, 245, 241, 0.24)' : '1px solid rgba(22, 27, 44, 0.14)',
                        color: isActive ? 'rgba(247, 245, 241, 0.8)' : 'var(--text-secondary)',
                      }}
                    >
                      {isActive ? 'ACTIVE' : 'MODE'}
                    </span>
                  </div>
                  <div
                    style={{
                      fontSize: '0.79rem',
                      lineHeight: 1.6,
                      color: isActive ? 'rgba(247, 245, 241, 0.82)' : 'var(--text-secondary)',
                    }}
                  >
                    {mode.description}
                  </div>
                </button>
              );
            })}
          </div>

          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: '1.5rem' }}>
            Tool availability follows the selected mode so the playground matches Hermes&apos; real backend contract.
          </div>

          <LabelText as="label" style={{ marginBottom: '0.75rem', display: 'block' }}>ANALYSIS DEPTH</LabelText>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem', marginBottom: '1.5rem' }}>
            <button
              type="button"
              className={agentAnalysisDepth === 'standard' ? 'btn-primary' : 'btn-secondary'}
              aria-pressed={agentAnalysisDepth === 'standard'}
              onClick={() => onAgentAnalysisDepthChange('standard')}
            >
              STANDARD
            </button>
            <button
              type="button"
              className={agentAnalysisDepth === 'deep' ? 'btn-primary' : 'btn-secondary'}
              aria-pressed={agentAnalysisDepth === 'deep'}
              onClick={() => onAgentAnalysisDepthChange('deep')}
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
              onChange={(event) => onAgentMaxStepsChange(parseInt(event.target.value, 10))}
            />
          </div>

          {agentExecutionMode === 'multimodal' && (
            <div style={{ marginBottom: '2rem' }}>
              <LabelText as="label" style={{ marginBottom: '0.85rem', display: 'block' }}>SCREENSHOT</LabelText>
              <input
                ref={fileInputRef}
                type="file"
                aria-label="Screenshot upload"
                accept="image/png,image/jpeg,image/webp"
                style={{ display: 'none' }}
                onChange={(event) => onFileSelection(event.target.files?.[0] || null)}
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
            <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>AVAILABLE TOOLS</LabelText>
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
          <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>PARAMETERS</LabelText>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Temperature</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{temperature.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="2" step="0.01" value={temperature} onChange={(event) => onTemperatureChange(parseFloat(event.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Max Tokens</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{maxTokens}</span>
            </div>
            <input type="range" min="1" max="8192" step="1" value={maxTokens} onChange={(event) => onMaxTokensChange(parseInt(event.target.value, 10))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Top P</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{topP.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="1" step="0.01" value={topP} onChange={(event) => onTopPChange(parseFloat(event.target.value))} />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <span style={{ fontSize: '0.85rem' }}>Frequency Penalty</span>
              <span className="mono" style={{ fontSize: '0.85rem' }}>{freqPenalty.toFixed(2)}</span>
            </div>
            <input type="range" min="0" max="2" step="0.01" value={freqPenalty} onChange={(event) => onFreqPenaltyChange(parseFloat(event.target.value))} />
          </div>

          <div style={{ marginTop: '2rem' }}>
            <LabelText as="label" style={{ marginBottom: '1rem', display: 'block' }}>SYSTEM PROMPT</LabelText>
            <textarea
              value={systemPrompt}
              onChange={(event) => onSystemPromptChange(event.target.value)}
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
}
