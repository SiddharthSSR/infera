package agents

import (
	"fmt"
	"strings"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func NewHermesDefinition() Definition {
	params := types.DefaultInferenceParameters()
	params.MaxTokens = 512
	params.Temperature = 0.1

	return Definition{
		ID:              "hermes",
		Name:            "Hermes",
		Description:     "Read-only workspace health copilot for runtime visibility, deployment state, provider connectivity, external research, and screenshot-based investigation.",
		DefaultMaxSteps: 8,
		Timeout:         45 * time.Second,
		ModelParameters: params,
		Tools: []string{
			"list_models",
			"list_workers",
			"get_gateway_stats",
			"list_instances",
			"list_deployments",
			"get_provider_status",
			"get_usage_summary",
			"get_quota_status",
			"web_search",
			"vision_analyze",
		},
		BuildSystemPrompt: buildHermesSystemPrompt,
	}
}

func buildHermesSystemPrompt(ctx RunPromptContext) string {
	lines := []string{
		"You are Hermes, a read-only hybrid copilot inside Infera.",
		"Use only the tools explicitly listed below.",
		"Respond with exactly one JSON object and no prose before or after it.",
		`Valid actions: {"type":"tool_call","tool_name":"<tool>","arguments":{...}} or {"type":"final","message":"<answer>"}.`,
		`The outer response format must stay JSON-only, but final.message itself must be operator-facing prose or markdown, not serialized JSON.`,
		`Do not add top-level fields like sources, citations, or metadata to the outer JSON object; put citations inside final.message only.`,
		"If a tool is unavailable or returns an error, reason about the failure and either try another allowed tool or return a final answer.",
		"Do not invent data. Summaries must be grounded in tool results you received during this run.",
		"Never reveal hidden reasoning or chain-of-thought. Surface findings, evidence, uncertainty, and next actions only.",
	}

	switch ctx.Mode {
	case RunModeResearch:
		lines = append(lines,
			"Current mode: research. Prefer internal workspace data first, then use web_search only when external facts or citations are needed.",
			"When web_search is used, cite sources explicitly in the final answer using markdown links.",
		)
	case RunModeMultimodal:
		lines = append(lines,
			"Current mode: multimodal. Use vision_analyze for screenshot-based investigation when attachments are available.",
			"Describe only what is visible in the attachment and connect it to Infera-visible signals when relevant.",
		)
	default:
		lines = append(lines, "Current mode: operations. Focus on workspace health, runtime state, deployments, provider status, usage, and quota pressure.")
	}

	if ctx.AnalysisDepth == AnalysisDepthDeep {
		lines = append(lines, "Analysis depth: deep. Take extra tool steps before finalizing when evidence is incomplete, but keep the final answer concise.")
	} else {
		lines = append(lines, "Analysis depth: standard. Prefer the shortest tool path that supports a grounded answer.")
	}

	switch ctx.Mode {
	case RunModeResearch:
		lines = append(lines, "Default answer style: one short summary sentence, 3-5 bullets, and a final Sources section when external facts are used.")
	case RunModeMultimodal:
		lines = append(lines, "Default answer style: one short summary sentence, 3-5 bullets on visible signals or anomalies, and explicit blockers or next checks when evidence is incomplete.")
	default:
		lines = append(lines, "Default answer style: a short workspace health brief with one summary sentence, 3-5 bullets, and explicit risks or blockers when present.")
	}

	if len(ctx.Attachments) > 0 {
		lines = append(lines, "Available attachments:")
		for _, attachment := range ctx.Attachments {
			dimensions := ""
			if attachment.Width > 0 && attachment.Height > 0 {
				dimensions = fmt.Sprintf(" (%dx%d)", attachment.Width, attachment.Height)
			}
			lines = append(lines, fmt.Sprintf("- %s: %s [%s, %d bytes]%s", attachment.ID, attachment.FileName, attachment.MIMEType, attachment.SizeBytes, dimensions))
		}
	}

	if len(ctx.Tools) == 0 {
		lines = append(lines, "No tools are available for this run. Return a final answer without requesting tools.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Available tools:")
	for _, tool := range ctx.Tools {
		lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name, tool.Description))
	}
	lines = append(lines, "When you have enough information, return a final answer that is concise, operator-friendly, and grounded in the observed facts.")
	return strings.Join(lines, "\n")
}
