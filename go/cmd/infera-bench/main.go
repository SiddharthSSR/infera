package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultTimeout = 60 * time.Second

type config struct {
	BaseURL     string
	APIKey      string
	APIKeyFile  string
	Model       string
	Workload    string
	Concurrency []int
	Requests    int
	Warmup      int
	Stream      bool
	Timeout     time.Duration
	OutJSON     string
	OutMD       string
}

type workload struct {
	Prompts []prompt `yaml:"prompts" json:"prompts"`
}

type prompt struct {
	ID          string    `yaml:"id" json:"id"`
	Messages    []message `yaml:"messages" json:"messages"`
	MaxTokens   int       `yaml:"max_tokens" json:"max_tokens"`
	Temperature float64   `yaml:"temperature" json:"temperature"`
	Tags        []string  `yaml:"tags" json:"tags"`
}

type message struct {
	Role    string `yaml:"role" json:"role"`
	Content string `yaml:"content" json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatResponse struct {
	Usage usage `json:"usage"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *usage `json:"usage,omitempty"`
}

type sample struct {
	LatencyMS float64
	TTFTMS    *float64
	TPOTMS    []float64
	Usage     *usage
	Error     string
}

type levelRun struct {
	Samples []sample
	Elapsed time.Duration
}

type report struct {
	RunID       string              `json:"run_id"`
	StartedAt   time.Time           `json:"started_at"`
	CompletedAt time.Time           `json:"completed_at"`
	BaseURL     string              `json:"base_url"`
	Model       string              `json:"model"`
	Workload    string              `json:"workload"`
	Streaming   bool                `json:"streaming"`
	GitCommit   string              `json:"git_commit,omitempty"`
	Results     []concurrencyResult `json:"concurrency_results"`
}

type concurrencyResult struct {
	Concurrency          int            `json:"concurrency"`
	Requests             int            `json:"requests"`
	Successes            int            `json:"successes"`
	Errors               int            `json:"errors"`
	ErrorRate            float64        `json:"error_rate"`
	RequestsPerSec       float64        `json:"requests_per_sec"`
	TokensPerSec         *float64       `json:"tokens_per_sec,omitempty"`
	LatencyMS            metricSummary  `json:"latency_ms"`
	TTFTMS               *metricSummary `json:"ttft_ms,omitempty"`
	TPOTMS               *metricSummary `json:"tpot_ms,omitempty"`
	RepresentativeErrors []string       `json:"representative_errors,omitempty"`
}

type metricSummary struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "infera-bench: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	wl, err := loadWorkload(cfg.Workload)
	if err != nil {
		return err
	}
	key, err := resolveAPIKey(cfg)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: cfg.Timeout}
	started := time.Now().UTC()
	rep := report{
		RunID:     "bench_" + started.Format("20060102_150405"),
		StartedAt: started,
		BaseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		Model:     cfg.Model,
		Workload:  cfg.Workload,
		Streaming: cfg.Stream,
		GitCommit: gitCommit(),
		Results:   make([]concurrencyResult, 0, len(cfg.Concurrency)),
	}

	for _, c := range cfg.Concurrency {
		if cfg.Warmup > 0 {
			_, err := runLevel(context.Background(), client, cfg, wl, key, c, cfg.Warmup)
			if err != nil {
				return fmt.Errorf("warmup concurrency %d: %w", c, err)
			}
		}
		result, err := runLevel(context.Background(), client, cfg, wl, key, c, cfg.Requests)
		if err != nil {
			return fmt.Errorf("benchmark concurrency %d: %w", c, err)
		}
		rep.Results = append(rep.Results, summarize(c, cfg.Requests, result))
	}

	rep.CompletedAt = time.Now().UTC()

	if cfg.OutJSON != "" {
		if err := writeJSON(cfg.OutJSON, rep); err != nil {
			return err
		}
	}
	md := renderMarkdown(rep)
	if cfg.OutMD != "" {
		if err := writeFile(cfg.OutMD, []byte(md)); err != nil {
			return err
		}
	}
	if cfg.OutJSON == "" && cfg.OutMD == "" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	if cfg.OutJSON != "" {
		fmt.Fprintf(stderr, "wrote JSON report: %s\n", cfg.OutJSON)
	}
	if cfg.OutMD != "" {
		fmt.Fprintf(stderr, "wrote Markdown report: %s\n", cfg.OutMD)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("infera-bench", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var concurrency string
	var timeout string
	cfg := config{}
	fs.StringVar(&cfg.BaseURL, "base-url", "http://127.0.0.1:8080", "Infera base URL")
	fs.StringVar(&cfg.APIKey, "api-key", "", "API key; prefer --api-key-file for shell history safety")
	fs.StringVar(&cfg.APIKeyFile, "api-key-file", "", "file containing API key")
	fs.StringVar(&cfg.Model, "model", "", "model ID")
	fs.StringVar(&cfg.Workload, "workload", "", "YAML workload path")
	fs.StringVar(&concurrency, "concurrency", "1", "comma-separated concurrency levels")
	fs.IntVar(&cfg.Requests, "requests", 3, "measured requests per concurrency level")
	fs.IntVar(&cfg.Warmup, "warmup", 0, "warmup requests per concurrency level")
	fs.BoolVar(&cfg.Stream, "stream", false, "use streaming chat completions")
	fs.StringVar(&timeout, "timeout", defaultTimeout.String(), "per-request timeout")
	fs.StringVar(&cfg.OutJSON, "out-json", "", "JSON report path")
	fs.StringVar(&cfg.OutMD, "out-md", "", "Markdown report path")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	levels, err := parseConcurrency(concurrency)
	if err != nil {
		return cfg, err
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return cfg, fmt.Errorf("parse --timeout: %w", err)
	}
	cfg.Concurrency = levels
	cfg.Timeout = d
	return cfg, nil
}

func (c config) validate() error {
	if strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("--base-url is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("--model is required")
	}
	if strings.TrimSpace(c.Workload) == "" {
		return errors.New("--workload is required")
	}
	if c.Requests <= 0 {
		return errors.New("--requests must be greater than zero")
	}
	if c.Warmup < 0 {
		return errors.New("--warmup cannot be negative")
	}
	if c.Timeout <= 0 {
		return errors.New("--timeout must be greater than zero")
	}
	if c.APIKey != "" && c.APIKeyFile != "" {
		return errors.New("use --api-key or --api-key-file, not both")
	}
	return nil
}

func parseConcurrency(input string) ([]int, error) {
	parts := strings.Split(input, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid concurrency level %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, errors.New("--concurrency must include at least one positive integer")
	}
	return out, nil
}

func loadWorkload(path string) (workload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return workload{}, fmt.Errorf("read workload: %w", err)
	}
	var wl workload
	if err := yaml.Unmarshal(data, &wl); err != nil {
		return workload{}, fmt.Errorf("parse workload YAML: %w", err)
	}
	if len(wl.Prompts) == 0 {
		return workload{}, errors.New("workload must contain at least one prompt")
	}
	for i, p := range wl.Prompts {
		if strings.TrimSpace(p.ID) == "" {
			return workload{}, fmt.Errorf("workload prompt %d missing id", i)
		}
		if len(p.Messages) == 0 {
			return workload{}, fmt.Errorf("workload prompt %q has no messages", p.ID)
		}
		if p.MaxTokens < 0 {
			return workload{}, fmt.Errorf("workload prompt %q has negative max_tokens", p.ID)
		}
	}
	return wl, nil
}

func resolveAPIKey(cfg config) (string, error) {
	if cfg.APIKey != "" {
		return strings.TrimSpace(cfg.APIKey), nil
	}
	if cfg.APIKeyFile == "" {
		return "", nil
	}
	data, err := os.ReadFile(cfg.APIKeyFile)
	if err != nil {
		return "", fmt.Errorf("read --api-key-file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func runLevel(ctx context.Context, client *http.Client, cfg config, wl workload, apiKey string, concurrency, total int) (levelRun, error) {
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/v1/chat/completions"
	jobs := make(chan int)
	results := make(chan sample, total)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				p := wl.Prompts[idx%len(wl.Prompts)]
				results <- runRequest(ctx, client, endpoint, cfg.Model, apiKey, p, cfg.Stream)
			}
		}()
	}
	start := time.Now()
	for i := 0; i < total; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(start)
	close(results)

	out := make([]sample, 0, total)
	for result := range results {
		out = append(out, result)
	}
	return levelRun{Samples: out, Elapsed: elapsed}, nil
}

func runRequest(ctx context.Context, client *http.Client, endpoint, model, apiKey string, p prompt, stream bool) sample {
	body := chatRequest{
		Model:       model,
		Messages:    p.Messages,
		MaxTokens:   p.MaxTokens,
		Temperature: p.Temperature,
		Stream:      stream,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return sample{Error: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return sample{Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return sample{LatencyMS: sinceMS(start), Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := readErrorBody(resp.Body)
		return sample{LatencyMS: sinceMS(start), Error: fmt.Sprintf("http %d: %s", resp.StatusCode, msg)}
	}
	if stream {
		return readStream(resp.Body, start)
	}
	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return sample{LatencyMS: sinceMS(start), Error: "decode response: " + err.Error()}
	}
	result := sample{LatencyMS: sinceMS(start)}
	if parsed.Usage.TotalTokens > 0 || parsed.Usage.PromptTokens > 0 || parsed.Usage.CompletionTokens > 0 {
		result.Usage = &parsed.Usage
	}
	return result
}

func readStream(body io.Reader, start time.Time) sample {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	done := false
	var ttft *float64
	var lastDelta time.Time
	tpot := []float64{}
	var bestUsage *usage

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return sample{LatencyMS: sinceMS(start), Error: "decode stream chunk: " + err.Error()}
		}
		if chunk.Usage != nil {
			bestUsage = mergeUsage(bestUsage, chunk.Usage)
		}
		if !chunkHasContent(chunk) {
			continue
		}
		now := time.Now()
		if ttft == nil {
			v := float64(now.Sub(start).Microseconds()) / 1000.0
			ttft = &v
		} else if !lastDelta.IsZero() {
			tpot = append(tpot, float64(now.Sub(lastDelta).Microseconds())/1000.0)
		}
		lastDelta = now
	}
	if err := scanner.Err(); err != nil {
		return sample{LatencyMS: sinceMS(start), TTFTMS: ttft, TPOTMS: tpot, Usage: bestUsage, Error: "read stream: " + err.Error()}
	}
	if !done {
		return sample{LatencyMS: sinceMS(start), TTFTMS: ttft, TPOTMS: tpot, Usage: bestUsage, Error: "stream ended without [DONE]"}
	}
	return sample{LatencyMS: sinceMS(start), TTFTMS: ttft, TPOTMS: tpot, Usage: bestUsage}
}

func chunkHasContent(chunk streamChunk) bool {
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			return true
		}
	}
	return false
}

func mergeUsage(current, next *usage) *usage {
	if next == nil {
		return current
	}
	if current == nil {
		copied := *next
		return &copied
	}
	if next.PromptTokens > current.PromptTokens {
		current.PromptTokens = next.PromptTokens
	}
	if next.CompletionTokens > current.CompletionTokens {
		current.CompletionTokens = next.CompletionTokens
	}
	if next.TotalTokens > current.TotalTokens {
		current.TotalTokens = next.TotalTokens
	}
	return current
}

func summarize(concurrency, requested int, run levelRun) concurrencyResult {
	samples := run.Samples
	latencies := make([]float64, 0, len(samples))
	ttfts := []float64{}
	tpots := []float64{}
	errorsOut := []string{}
	successes := 0
	totalTokens := 0

	for _, s := range samples {
		if s.LatencyMS > 0 {
			latencies = append(latencies, s.LatencyMS)
		}
		if s.Error != "" {
			if len(errorsOut) < 5 {
				errorsOut = append(errorsOut, s.Error)
			}
			continue
		}
		successes++
		if s.TTFTMS != nil {
			ttfts = append(ttfts, *s.TTFTMS)
		}
		tpots = append(tpots, s.TPOTMS...)
		if s.Usage != nil {
			totalTokens += s.Usage.TotalTokens
		}
	}

	errorsCount := len(samples) - successes
	elapsedSeconds := run.Elapsed.Seconds()
	result := concurrencyResult{
		Concurrency:          concurrency,
		Requests:             requested,
		Successes:            successes,
		Errors:               errorsCount,
		ErrorRate:            ratio(errorsCount, float64(len(samples))),
		RequestsPerSec:       ratio(successes, elapsedSeconds),
		LatencyMS:            summarizeMetric(latencies),
		RepresentativeErrors: errorsOut,
	}
	if totalTokens > 0 && elapsedSeconds > 0 {
		v := float64(totalTokens) / elapsedSeconds
		result.TokensPerSec = &v
	}
	if len(ttfts) > 0 {
		v := summarizeMetric(ttfts)
		result.TTFTMS = &v
	}
	if len(tpots) > 0 {
		v := summarizeMetric(tpots)
		result.TPOTMS = &v
	}
	return result
}

func summarizeMetric(values []float64) metricSummary {
	if len(values) == 0 {
		return metricSummary{}
	}
	sort.Float64s(values)
	return metricSummary{
		P50: percentile(values, 0.50),
		P95: percentile(values, 0.95),
		P99: percentile(values, 0.99),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func renderMarkdown(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Infera Benchmark Report\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", rep.RunID)
	fmt.Fprintf(&b, "- Started: `%s`\n", rep.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Completed: `%s`\n", rep.CompletedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Base URL: `%s`\n", rep.BaseURL)
	fmt.Fprintf(&b, "- Model: `%s`\n", rep.Model)
	fmt.Fprintf(&b, "- Workload: `%s`\n", rep.Workload)
	fmt.Fprintf(&b, "- Streaming: `%t`\n", rep.Streaming)
	if rep.GitCommit != "" {
		fmt.Fprintf(&b, "- Git commit: `%s`\n", rep.GitCommit)
	}
	fmt.Fprintf(&b, "\n## Results\n\n")
	fmt.Fprintf(&b, "| Concurrency | Requests | Successes | Errors | Error Rate | Req/s | Tok/s | Latency p50/p95/p99 ms | TTFT p50/p95/p99 ms | TPOT p50/p95/p99 ms |\n")
	fmt.Fprintf(&b, "| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- |\n")
	for _, r := range rep.Results {
		fmt.Fprintf(&b, "| %d | %d | %d | %d | %.2f%% | %.2f | %s | %s | %s | %s |\n",
			r.Concurrency,
			r.Requests,
			r.Successes,
			r.Errors,
			r.ErrorRate*100,
			r.RequestsPerSec,
			formatOptionalFloat(r.TokensPerSec),
			formatMetric(r.LatencyMS),
			formatOptionalMetric(r.TTFTMS),
			formatOptionalMetric(r.TPOTMS),
		)
	}
	fmt.Fprintf(&b, "\n## Notable Errors\n\n")
	wroteError := false
	for _, r := range rep.Results {
		for _, msg := range r.RepresentativeErrors {
			wroteError = true
			fmt.Fprintf(&b, "- concurrency %d: `%s`\n", r.Concurrency, sanitizeInline(msg))
		}
	}
	if !wroteError {
		fmt.Fprintf(&b, "No representative errors recorded.\n")
	}
	fmt.Fprintf(&b, "\n## MVP Limitations\n\n")
	fmt.Fprintf(&b, "- Cost metrics are not implemented yet.\n")
	fmt.Fprintf(&b, "- Route decision metrics are not implemented yet.\n")
	fmt.Fprintf(&b, "- TTFT is measured only for streaming responses with a non-empty content delta.\n")
	fmt.Fprintf(&b, "- TPOT is approximate in streaming mode because token boundaries are inferred from content chunks unless usage metadata is present.\n")
	return b.String()
}

func writeJSON(path string, rep report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readErrorBody(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 4096))
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		return "empty response body"
	}
	return msg
}

func gitCommit() string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sinceMS(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

func ratio(n int, d float64) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / d
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func formatMetric(m metricSummary) string {
	return fmt.Sprintf("%.1f / %.1f / %.1f", m.P50, m.P95, m.P99)
}

func formatOptionalMetric(m *metricSummary) string {
	if m == nil {
		return "n/a"
	}
	return formatMetric(*m)
}

func formatOptionalFloat(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *v)
}

func sanitizeInline(s string) string {
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
