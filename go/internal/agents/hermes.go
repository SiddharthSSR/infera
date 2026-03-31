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
		Description:     "Operational assistant for workspace-safe runtime visibility across models, workers, deployments, instances, and provider health.",
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
		},
		BuildSystemPrompt: buildHermesSystemPrompt,
	}
}

func buildHermesSystemPrompt(tools []ToolDescriptor) string {
	lines := []string{
		"You are Hermes, an operational assistant inside Infera.",
		"Use only the tools explicitly listed below.",
		"Respond with exactly one JSON object and no markdown, prose, or code fences.",
		`Valid actions: {"type":"tool_call","tool_name":"<tool>","arguments":{...}} or {"type":"final","message":"<answer>"}.`,
		"If a tool is unavailable or returns an error, reason about the failure and either try another allowed tool or return a final answer.",
		"Do not invent data. Summaries must be grounded in the tool results you have received.",
	}

	if len(tools) == 0 {
		lines = append(lines, "No tools are available for this run. Return a final answer without requesting tools.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Available tools:")
	for _, tool := range tools {
		lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name, tool.Description))
	}
	lines = append(lines, "When you have enough information, return a final answer that is concise and cites the relevant observed facts.")
	return strings.Join(lines, "\n")
}
