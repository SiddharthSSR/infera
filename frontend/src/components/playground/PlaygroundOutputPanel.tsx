import { Suspense } from 'react';
import type { RefObject } from 'react';
import { CollapsibleSection } from '../CollapsibleSection';
import { LabelText } from '../shared';
import { summarizeAgentResult, type TokenUsage } from '../../hooks/usePlaygroundExecutionState';
import { formatAgentStatus, formatStepPayload, formatStepType } from '../../lib/labels';
import { lazyWithRetry } from '../../lib/lazyWithRetry';
import type {
  AgentAnalysisDepth,
  AgentExecutionMode,
  AgentRunDetail,
  AgentRunStatus,
} from '../../types';

interface AgentThinkingState {
  headline: string;
  detail: string;
  recentChecks: string[];
}

const terminalAgentStatuses = new Set<AgentRunStatus>(['succeeded', 'failed', 'canceled']);
const MarkdownOutput = lazyWithRetry(
  () => import('../MarkdownOutput').then((module) => ({ default: module.MarkdownOutput })),
  'playground-markdown-output',
);

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

function friendlyToolLabel(toolName?: string) {
  if (!toolName) {
    return 'workspace state';
  }
  return toolLabelMap[toolName] || toolName.replace(/_/g, ' ');
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

function MarkdownOutputFallback({ content }: { content: string }) {
  return (
    <div
      className="markdown-output"
      style={{
        whiteSpace: 'pre-wrap',
        lineHeight: 1.7,
        color: 'var(--text-primary)',
      }}
    >
      {content}
    </div>
  );
}

function DeferredMarkdownOutput({ content }: { content: string }) {
  return (
    <Suspense fallback={<MarkdownOutputFallback content={content} />}>
      <MarkdownOutput content={content} />
    </Suspense>
  );
}

interface PlaygroundOutputPanelProps {
  isAgentMode: boolean;
  isExtraSmall: boolean;
  isMobile: boolean;
  isLoading: boolean;
  responseRef: RefObject<HTMLDivElement>;
  response: string;
  tokenUsage: TokenUsage | null;
  agentDetail: AgentRunDetail | null;
  agentExecutionMode: AgentExecutionMode;
  agentAnalysisDepth: AgentAnalysisDepth;
}

export function PlaygroundOutputPanel({
  isAgentMode,
  isExtraSmall,
  isMobile,
  isLoading,
  responseRef,
  response,
  tokenUsage,
  agentDetail,
  agentExecutionMode,
  agentAnalysisDepth,
}: PlaygroundOutputPanelProps) {
  const thinkingState = deriveAgentThinking(
    agentDetail,
    isAgentMode && (isLoading || agentDetail?.run.status === 'queued' || agentDetail?.run.status === 'running'),
  );
  const terminalRun = agentDetail && terminalAgentStatuses.has(agentDetail.run.status);
  const traceExpandedByDefault = agentDetail?.run.status === 'failed' || agentDetail?.run.status === 'canceled';

  return (
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
          padding: isExtraSmall ? '0.5rem 0.75rem 0.35rem' : isMobile ? '0.75rem 1rem 0.5rem' : '0.85rem 1.5rem 0.4rem',
          flexShrink: 0,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: isExtraSmall ? 'flex-start' : 'center',
          flexDirection: isExtraSmall ? 'column' : 'row',
          flexWrap: 'wrap',
          gap: '0.5rem',
        }}
      >
        <LabelText as="label">OUTPUT</LabelText>
        {tokenUsage && !isAgentMode && (
          <div
            className="mono"
            style={{
              fontSize: isExtraSmall ? '0.6rem' : '0.65rem',
              color: 'var(--text-secondary)',
              display: 'flex',
              gap: isExtraSmall ? '0.5rem' : '1rem',
              flexWrap: 'wrap',
            }}
          >
            <span>{tokenUsage.promptTokens} prompt</span>
            <span>{tokenUsage.completionTokens} completion</span>
            {!isExtraSmall && <span>{tokenUsage.totalTokens} total</span>}
            <span>{tokenUsage.tokensPerSec.toFixed(1)} tok/s</span>
          </div>
        )}
      </div>

      <div
        ref={responseRef}
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: isExtraSmall ? '0.35rem 0.75rem 0.75rem' : isMobile ? '0.5rem 1rem 1rem' : '0.45rem 1.5rem 1.5rem',
          minHeight: 0,
        }}
      >
        {isAgentMode ? (
          <>
            {thinkingState ? (
              <section
                className="animate-fade-in"
                style={{
                  border: '1px solid var(--border-color)',
                  padding: isExtraSmall ? '0.85rem' : '1.25rem',
                  background: 'rgba(244, 242, 238, 0.7)',
                  marginBottom: '1rem',
                }}
              >
                <div className="mono" style={{ fontSize: '0.68rem', marginBottom: '0.6rem', color: 'var(--text-secondary)' }}>LIVE REVIEW</div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '0.65rem' }}>
                  <div style={{ display: 'flex', gap: '0.35rem' }} aria-hidden="true">
                    <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-primary)', opacity: 0.35 }} />
                    <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-primary)', opacity: 0.55 }} />
                    <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-primary)', opacity: 0.75 }} />
                  </div>
                  <div style={{ fontSize: isExtraSmall ? '0.95rem' : '1.1rem', fontWeight: 600 }}>{thinkingState.headline}</div>
                </div>
                <p style={{ color: 'var(--text-secondary)', lineHeight: 1.7, marginBottom: '0.85rem', fontSize: isExtraSmall ? '0.85rem' : undefined }}>{thinkingState.detail}</p>
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
              <section
                className="animate-fade-in"
                style={{
                  border: '1px solid var(--border-color)',
                  padding: isExtraSmall ? '0.85rem' : '1.25rem',
                  background: agentDetail.run.status === 'succeeded' ? 'white' : 'rgba(255, 246, 242, 0.9)',
                  marginBottom: '1rem',
                }}
              >
                <div className="mono" style={{ fontSize: '0.68rem', marginBottom: '0.75rem', color: 'var(--text-secondary)', display: 'flex', gap: isExtraSmall ? '0.5rem' : '0.85rem', flexWrap: 'wrap' }}>
                  <span>{formatAgentStatus(agentDetail.run.status)}</span>
                  <span>{agentExecutionMode.toUpperCase()}</span>
                  <span>{agentAnalysisDepth.toUpperCase()}</span>
                  <span>{agentDetail.steps.length} STEP{agentDetail.steps.length === 1 ? '' : 'S'}</span>
                </div>
                <DeferredMarkdownOutput content={summarizeAgentResult(agentDetail)} />
                {agentDetail.sources && agentDetail.sources.length > 0 && (
                  <div style={{ marginTop: '1rem', display: 'grid', gap: '0.75rem' }}>
                    <LabelText as="div">SOURCES</LabelText>
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
              <section className="animate-fade-in" style={{ border: '1px solid var(--border-color)', padding: isExtraSmall ? '0.85rem' : '1.25rem', background: 'white', marginBottom: '1rem' }}>
                <DeferredMarkdownOutput content={response} />
              </section>
            ) : (
              <span style={{ color: 'var(--text-secondary)', fontSize: isExtraSmall ? '0.85rem' : undefined }}>
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
                              padding: isExtraSmall ? '0.5rem' : '0.85rem',
                              overflowX: 'auto',
                              whiteSpace: 'pre-wrap',
                              wordBreak: 'break-word',
                              fontFamily: 'var(--font-mono)',
                              fontSize: isExtraSmall ? '0.68rem' : '0.78rem',
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
            <DeferredMarkdownOutput content={response} />
          </div>
        ) : (
          <span style={{ color: 'var(--text-secondary)', fontSize: isExtraSmall ? '0.85rem' : undefined }}>
            Model output will appear here.
          </span>
        )}
      </div>
    </section>
  );
}
