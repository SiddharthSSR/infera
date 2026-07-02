package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConcurrency(t *testing.T) {
	got, err := parseConcurrency("1, 4,8")
	if err != nil {
		t.Fatalf("parseConcurrency returned error: %v", err)
	}
	want := []int{1, 4, 8}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseConcurrencyRejectsInvalidLevel(t *testing.T) {
	if _, err := parseConcurrency("1,0"); err == nil {
		t.Fatal("expected invalid concurrency error")
	}
}

func TestLoadWorkload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workload.yaml")
	data := []byte(`
prompts:
  - id: hello
    messages:
      - role: user
        content: Say hello.
    max_tokens: 8
    temperature: 0.1
    tags: [short]
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	wl, err := loadWorkload(path)
	if err != nil {
		t.Fatalf("loadWorkload returned error: %v", err)
	}
	if len(wl.Prompts) != 1 || wl.Prompts[0].ID != "hello" {
		t.Fatalf("unexpected workload: %+v", wl)
	}
}

func TestSummarizeComputesPercentilesAndErrorRate(t *testing.T) {
	ttft := 10.0
	samples := []sample{
		{LatencyMS: 100, TTFTMS: &ttft, TPOTMS: []float64{5, 6}, Usage: &usage{TotalTokens: 10}},
		{LatencyMS: 200, Usage: &usage{TotalTokens: 20}},
		{LatencyMS: 300, Error: "boom"},
	}
	got := summarize(2, 3, levelRun{Samples: samples, Elapsed: time.Second})
	if got.Successes != 2 || got.Errors != 1 {
		t.Fatalf("unexpected success/error counts: %+v", got)
	}
	if got.ErrorRate < 0.33 || got.ErrorRate > 0.34 {
		t.Fatalf("unexpected error rate: %f", got.ErrorRate)
	}
	if got.LatencyMS.P50 != 200 || got.LatencyMS.P95 != 300 || got.LatencyMS.P99 != 300 {
		t.Fatalf("unexpected latency percentiles: %+v", got.LatencyMS)
	}
	if got.TTFTMS == nil || got.TTFTMS.P50 != 10 {
		t.Fatalf("unexpected ttft summary: %+v", got.TTFTMS)
	}
	if got.TPOTMS == nil || got.TPOTMS.P50 != 5 || got.TPOTMS.P95 != 6 {
		t.Fatalf("unexpected tpot summary: %+v", got.TPOTMS)
	}
}

func TestRenderMarkdownIncludesLimitations(t *testing.T) {
	rep := report{
		RunID:     "bench_test",
		BaseURL:   "https://example.test",
		Model:     "model",
		Workload:  "workload.yaml",
		Streaming: true,
		Results: []concurrencyResult{{
			Concurrency:    1,
			Requests:       1,
			Successes:      1,
			LatencyMS:      metricSummary{P50: 1, P95: 1, P99: 1},
			RequestsPerSec: 1,
		}},
	}
	md := renderMarkdown(rep)
	for _, want := range []string{"# Infera Benchmark Report", "Cost metrics are not implemented yet", "Route decision metrics are not implemented yet"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestRunRequestUsesAPIKeyAndParsesUsage(t *testing.T) {
	const secret = "inf_secret_test"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Fatalf("unexpected auth header %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser(`{"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7},"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`),
		}, nil
	})}

	got := runRequest(context.Background(), client, "https://example.test/v1/chat/completions", "test-model", secret, prompt{
		ID: "hello",
		Messages: []message{{
			Role:    "user",
			Content: "Say hello.",
		}},
		MaxTokens:   8,
		Temperature: 0.1,
	}, false)
	if got.Error != "" {
		t.Fatalf("runRequest returned error: %s", got.Error)
	}
	if got.Usage == nil || got.Usage.TotalTokens != 7 {
		t.Fatalf("unexpected usage: %+v", got.Usage)
	}

	rep := report{
		RunID:    "bench_test",
		BaseURL:  "https://example.test",
		Model:    "test-model",
		Workload: "workload.yaml",
		Results: []concurrencyResult{{
			Concurrency:    1,
			Requests:       1,
			Successes:      1,
			LatencyMS:      metricSummary{P50: 1, P95: 1, P99: 1},
			RequestsPerSec: 1,
		}},
	}
	var jsonReport bytes.Buffer
	enc := json.NewEncoder(&jsonReport)
	if err := enc.Encode(rep); err != nil {
		t.Fatal(err)
	}
	mdReport := renderMarkdown(rep)
	if strings.Contains(jsonReport.String(), secret) || strings.Contains(mdReport, secret) {
		t.Fatal("reports must not contain API key")
	}
}

func TestReadStreamMeasuresTTFTAndRequiresDone(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: [DONE]\n\n")
	got := readStream(body, nowForTest())
	if got.Error != "" {
		t.Fatalf("readStream returned error: %s", got.Error)
	}
	if got.TTFTMS == nil {
		t.Fatal("expected TTFT")
	}
	if len(got.TPOTMS) != 1 {
		t.Fatalf("expected one TPOT sample, got %d", len(got.TPOTMS))
	}

	incomplete := readStream(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"), nowForTest())
	if !strings.Contains(incomplete.Error, "without [DONE]") {
		t.Fatalf("expected incomplete stream error, got %q", incomplete.Error)
	}
}

func writeTestWorkload(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "workload.yaml")
	data := []byte(`
prompts:
  - id: hello
    messages:
      - role: user
        content: Say hello.
    max_tokens: 8
    temperature: 0.1
    tags: [short]
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func nowForTest() time.Time {
	return time.Now().Add(-time.Millisecond)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func ioNopCloser(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}
