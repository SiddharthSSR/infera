package main

import (
	"bytes"
	"context"
	"encoding/base64"
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
	got := summarize(2, 3, levelRun{Samples: samples, Elapsed: time.Second}, false)
	if got.Successes != 2 || got.Errors != 1 {
		t.Fatalf("unexpected success/error counts: %+v", got)
	}
	if got.ErrorRate < 0.33 || got.ErrorRate > 0.34 {
		t.Fatalf("unexpected error rate: %f", got.ErrorRate)
	}
	if got.LatencyMS.P50 != 100 || got.LatencyMS.P95 != 200 || got.LatencyMS.P99 != 200 {
		t.Fatalf("unexpected latency percentiles: %+v", got.LatencyMS)
	}
	if got.FailedLatencyMS == nil || got.FailedLatencyMS.P50 != 300 {
		t.Fatalf("unexpected failed latency percentiles: %+v", got.FailedLatencyMS)
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
	for _, want := range []string{"# Infera Benchmark Report", "Cost metrics are not implemented yet", "Route decision metadata is available only when `--capture-route-decision` is enabled", "Approx TPOT", "Latency summarizes successful requests only"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestSummarizeFailedRequestLatencyDoesNotAffectSuccessLatency(t *testing.T) {
	got := summarize(1, 3, levelRun{
		Elapsed: time.Second,
		Samples: []sample{
			{LatencyMS: 100},
			{LatencyMS: 120},
			{LatencyMS: 1, Error: "fast failure"},
		},
	}, false)
	if got.LatencyMS.P50 != 100 || got.LatencyMS.P95 != 120 || got.LatencyMS.P99 != 120 {
		t.Fatalf("successful latency included failure sample: %+v", got.LatencyMS)
	}
	if got.FailedLatencyMS == nil {
		t.Fatal("expected failed latency summary")
	}
	if got.FailedLatencyMS.P50 != 1 || got.FailedLatencyMS.P95 != 1 || got.FailedLatencyMS.P99 != 1 {
		t.Fatalf("unexpected failed latency summary: %+v", got.FailedLatencyMS)
	}
	if got.ErrorRate < 0.33 || got.ErrorRate > 0.34 {
		t.Fatalf("unexpected error rate: %f", got.ErrorRate)
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
	}, false, false)
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

func TestRunRequestRejectsNonStreamingResponseWithoutChoices(t *testing.T) {
	client := &http.Client{Transport: staticJSONTransport(`{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)}
	got := runRequest(context.Background(), client, "https://example.test/v1/chat/completions", "test-model", "", prompt{
		ID:       "hello",
		Messages: []message{{Role: "user", Content: "Say hello."}},
	}, false, false)
	if !strings.Contains(got.Error, "missing choices") {
		t.Fatalf("expected missing choices error, got %q", got.Error)
	}
}

func TestRunRequestRejectsNonStreamingResponseWithEmptyChoice(t *testing.T) {
	client := &http.Client{Transport: staticJSONTransport(`{"choices":[{"message":{"role":"assistant","content":""}}]}`)}
	got := runRequest(context.Background(), client, "https://example.test/v1/chat/completions", "test-model", "", prompt{
		ID:       "hello",
		Messages: []message{{Role: "user", Content: "Say hello."}},
	}, false, false)
	if !strings.Contains(got.Error, "did not include assistant content") {
		t.Fatalf("expected empty choice error, got %q", got.Error)
	}
}

func TestRunRequestAcceptsNonStreamingResponseWithChoiceContent(t *testing.T) {
	client := &http.Client{Transport: staticJSONTransport(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)}
	got := runRequest(context.Background(), client, "https://example.test/v1/chat/completions", "test-model", "", prompt{
		ID:       "hello",
		Messages: []message{{Role: "user", Content: "Say hello."}},
	}, false, false)
	if got.Error != "" {
		t.Fatalf("expected success, got error %q", got.Error)
	}
}

func TestRunRequestCapturesRouteDecisionWhenEnabled(t *testing.T) {
	routeHeader := encodeTestRouteDecisionHeader(t, routeDecisionMetadata{
		RequestID:           "req-test",
		Model:               "test-model",
		Strategy:            "least_loaded",
		SelectedWorker:      "worker-1",
		SelectedProvider:    "runpod",
		CandidatesEvaluated: intPtr(2),
	})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Infera-Debug-Route"); got != "true" {
			t.Fatalf("expected debug route header, got %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":            []string{"application/json"},
				"X-Infera-Route-Decision": []string{routeHeader},
			},
			Body: ioNopCloser(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		}, nil
	})}
	got := runRequest(context.Background(), client, "https://example.test/v1/chat/completions", "test-model", "secret", prompt{
		ID:       "hello",
		Messages: []message{{Role: "user", Content: "do not expose"}},
	}, false, true)
	if got.Error != "" {
		t.Fatalf("expected success, got error %q", got.Error)
	}
	if got.RouteDecision == nil {
		t.Fatal("expected route decision metadata")
	}
	if got.RouteDecision.Strategy != "least_loaded" || got.RouteDecision.SelectedWorker != "worker-1" {
		t.Fatalf("unexpected route decision: %+v", got.RouteDecision)
	}
}

func TestRunRequestDoesNotRequestRouteDecisionByDefault(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Infera-Debug-Route"); got != "" {
			t.Fatalf("expected no debug route header by default, got %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		}, nil
	})}
	got := runRequest(context.Background(), client, "https://example.test/v1/chat/completions", "test-model", "", prompt{
		ID:       "hello",
		Messages: []message{{Role: "user", Content: "Say hello."}},
	}, false, false)
	if got.Error != "" {
		t.Fatalf("expected success, got error %q", got.Error)
	}
	if got.RouteDecision != nil {
		t.Fatalf("expected no route decision metadata by default, got %+v", got.RouteDecision)
	}
}

func TestSummarizeAndMarkdownReportRouteDecisionMetadata(t *testing.T) {
	candidates := 2
	got := summarize(1, 3, levelRun{
		Elapsed: time.Second,
		Samples: []sample{
			{LatencyMS: 100, RouteDecision: &routeDecisionMetadata{Strategy: "least_loaded", SelectedWorker: "worker-1", CandidatesEvaluated: &candidates}},
			{LatencyMS: 110, RouteDecision: &routeDecisionMetadata{Strategy: "affinity", SelectedWorker: "worker-1", CandidatesEvaluated: &candidates}},
			{LatencyMS: 120},
		},
	}, true)
	if got.RouteDecision == nil {
		t.Fatal("expected route decision summary")
	}
	if got.RouteDecision.StrategiesObserved["least_loaded"] != 1 || got.RouteDecision.StrategiesObserved["affinity"] != 1 {
		t.Fatalf("unexpected strategies: %+v", got.RouteDecision.StrategiesObserved)
	}
	if got.RouteDecision.MissingMetadataCount != 1 {
		t.Fatalf("unexpected missing metadata count: %d", got.RouteDecision.MissingMetadataCount)
	}
	if got.RouteDecision.CandidatesEvaluated == nil || got.RouteDecision.CandidatesEvaluated.P50 != 2 {
		t.Fatalf("unexpected candidate summary: %+v", got.RouteDecision.CandidatesEvaluated)
	}
	md := renderMarkdown(report{
		RunID:                "bench_routes",
		CaptureRouteDecision: true,
		Results:              []concurrencyResult{got},
	})
	for _, want := range []string{"## Route Decisions", "least_loaded=1", "affinity=1", "worker-1=2", "Missing route metadata"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
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

func staticJSONTransport(body string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser(body),
		}, nil
	})
}

func encodeTestRouteDecisionHeader(t *testing.T, metadata routeDecisionMetadata) string {
	t.Helper()
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func intPtr(v int) *int {
	return &v
}
