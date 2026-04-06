package gateway

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/auth"
)

// ---------------------------------------------------------------------------
// URL allowlist validation
// ---------------------------------------------------------------------------

func TestIsAllowedResearchDomain(t *testing.T) {
	allowed := []string{"runpod.io", "docs.vllm.ai", "anthropic.com"}

	tests := []struct {
		host string
		want bool
	}{
		{"runpod.io", true},
		{"status.runpod.io", true},
		{"docs.vllm.ai", true},
		{"anthropic.com", true},
		{"blog.anthropic.com", true},
		{"evil-runpod.io", false},
		{"example.com", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			got := isAllowedResearchDomain(tc.host, allowed)
			if got != tc.want {
				t.Fatalf("isAllowedResearchDomain(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestIsAllowedResearchDomainEmptyAllowlist(t *testing.T) {
	if isAllowedResearchDomain("runpod.io", nil) {
		t.Fatal("expected no domain allowed with nil allowlist")
	}
	if isAllowedResearchDomain("runpod.io", []string{}) {
		t.Fatal("expected no domain allowed with empty allowlist")
	}
}

// ---------------------------------------------------------------------------
// Search result HTML parsing
// ---------------------------------------------------------------------------

func TestParseDuckDuckGoResultsFiltersToAllowlistedDomains(t *testing.T) {
	htmlPayload := `
<div class="result">
  <a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fstatus.runpod.io%2F">RunPod Status</a>
  <div class="result__snippet">Official status page.</div>
</div>
<div class="result">
  <a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Frandom-blog.example%2Fincident">Random Blog</a>
  <div class="result__snippet">Unofficial commentary.</div>
</div>`

	results := parseDuckDuckGoResults(htmlPayload, []string{"status.runpod.io"}, 5)
	if len(results) != 1 {
		t.Fatalf("expected one allowlisted result, got %+v", results)
	}
	if results[0].Domain != "status.runpod.io" {
		t.Fatalf("expected allowlisted domain, got %+v", results[0])
	}
}

func TestParseDuckDuckGoResultsRespectsMaxResults(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 5; i++ {
		encoded := url.QueryEscape(fmt.Sprintf("https://docs.vllm.ai/page%d", i))
		fmt.Fprintf(&b, `<a class="result__a" href="https://duckduckgo.com/l/?uddg=%s">Page %d</a>
<div class="result__snippet">Snippet %d</div>`, encoded, i, i)
	}
	results := parseDuckDuckGoResults(b.String(), []string{"docs.vllm.ai"}, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (maxResults=2), got %d", len(results))
	}
}

func TestParseDuckDuckGoResultsEmptyPayload(t *testing.T) {
	results := parseDuckDuckGoResults("", []string{"runpod.io"}, 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty HTML, got %d", len(results))
	}
}

func TestParseDuckDuckGoResultsNoAllowedResults(t *testing.T) {
	htmlPayload := `
<a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage">Example</a>
<div class="result__snippet">Not allowed.</div>`

	results := parseDuckDuckGoResults(htmlPayload, []string{"runpod.io"}, 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results when no domains match, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// normalizeSearchResultURL
// ---------------------------------------------------------------------------

func TestNormalizeSearchResultURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "DDG redirect URL with uddg param",
			raw:  "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fstatus.runpod.io%2F",
			want: "https://status.runpod.io/",
		},
		{
			name: "protocol-relative URL",
			raw:  "//docs.vllm.ai/page",
			want: "https://docs.vllm.ai/page",
		},
		{
			name: "direct URL",
			raw:  "https://anthropic.com/research",
			want: "https://anthropic.com/research",
		},
		{
			name: "HTML entity escaped URL",
			raw:  "https://example.com/path?a=1&amp;b=2",
			want: "https://example.com/path?a=1&b=2",
		},
		{
			name: "whitespace trimmed",
			raw:  "  https://anthropic.com  ",
			want: "https://anthropic.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSearchResultURL(tc.raw)
			if got != tc.want {
				t.Fatalf("normalizeSearchResultURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// stripHTML
// ---------------------------------------------------------------------------

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<b>bold</b> text", "bold text"},
		{"no tags", "no tags"},
		{"&amp; entity", "& entity"},
		{"<span class=\"x\">nested <em>tags</em></span>", "nested tags"},
	}
	for _, tc := range tests {
		got := stripHTML(tc.input)
		if got != tc.want {
			t.Fatalf("stripHTML(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Search query validation
// ---------------------------------------------------------------------------

func TestSearchEmptyQueryReturnsError(t *testing.T) {
	searcher := newDuckDuckGoWebSearcher().(*duckDuckGoWebSearcher)
	_, err := searcher.Search(context.Background(), WebSearchRequest{Query: ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchWhitespaceOnlyQueryReturnsError(t *testing.T) {
	searcher := newDuckDuckGoWebSearcher().(*duckDuckGoWebSearcher)
	_, err := searcher.Search(context.Background(), WebSearchRequest{Query: "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only query")
	}
}

// ---------------------------------------------------------------------------
// Web search with mock HTTP
// ---------------------------------------------------------------------------

func TestSearchNetworkError(t *testing.T) {
	searcher := &duckDuckGoWebSearcher{
		client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("simulated network error")
			}),
		},
		allowedDomains: []string{"runpod.io"},
	}

	_, err := searcher.Search(context.Background(), WebSearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error on network failure")
	}
	if !strings.Contains(err.Error(), "simulated network error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchSuccessfulWithMockServer(t *testing.T) {
	encoded := url.QueryEscape("https://docs.vllm.ai/getting-started")
	htmlBody := fmt.Sprintf(`<a class="result__a" href="https://duckduckgo.com/l/?uddg=%s">vLLM Docs</a>
<div class="result__snippet">Getting started with vLLM.</div>`, encoded)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, htmlBody)
	}))
	defer srv.Close()

	searcher := &duckDuckGoWebSearcher{
		client:         srv.Client(),
		allowedDomains: []string{"docs.vllm.ai"},
	}
	searcher.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL, _ = url.Parse(srv.URL + r.URL.Path + "?" + r.URL.RawQuery)
		return http.DefaultTransport.RoundTrip(r)
	})

	results, err := searcher.Search(context.Background(), WebSearchRequest{Query: "vllm getting started", MaxResults: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Domain != "docs.vllm.ai" {
		t.Fatalf("unexpected domain: %q", results[0].Domain)
	}
}

// ---------------------------------------------------------------------------
// Vision analyze: error paths
// ---------------------------------------------------------------------------

func TestVisionAnalyzeNilAttachmentReturnsError(t *testing.T) {
	analyzer := newScreenshotAnalyzer()
	_, err := analyzer.Analyze(context.Background(), VisionAnalyzeRequest{
		Attachment: nil,
		Question:   "What is in this image?",
	})
	if err == nil {
		t.Fatal("expected error for nil attachment")
	}
}

func TestVisionAnalyzeEmptyStoragePathReturnsError(t *testing.T) {
	analyzer := newScreenshotAnalyzer()
	_, err := analyzer.Analyze(context.Background(), VisionAnalyzeRequest{
		Attachment: &agents.Attachment{
			ID:          "att-1",
			FileName:    "screenshot.png",
			MIMEType:    "image/png",
			StoragePath: "",
		},
		Question: "What is in this image?",
	})
	if err == nil {
		t.Fatal("expected error for empty storage path")
	}
}

func TestVisionAnalyzeFileNotFoundReturnsError(t *testing.T) {
	analyzer := newScreenshotAnalyzer()
	_, err := analyzer.Analyze(context.Background(), VisionAnalyzeRequest{
		Attachment: &agents.Attachment{
			ID:          "att-1",
			FileName:    "screenshot.png",
			MIMEType:    "image/png",
			StoragePath: "/nonexistent/path/image.png",
		},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// Vision analyze: valid image
// ---------------------------------------------------------------------------

func createTestPNG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for x := 0; x < 100; x++ {
		for y := 0; y < 50; y++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create test png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return p
}

func TestVisionAnalyzeWithValidImage(t *testing.T) {
	pngPath := createTestPNG(t)
	fi, err := os.Stat(pngPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	analyzer := newScreenshotAnalyzer()
	result, err := analyzer.Analyze(context.Background(), VisionAnalyzeRequest{
		Attachment: &agents.Attachment{
			ID:          "att-1",
			FileName:    "test.png",
			MIMEType:    "image/png",
			SizeBytes:   fi.Size(),
			StoragePath: pngPath,
		},
		Question: "What color is the image?",
		Focus:    "color analysis",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["question"] != "What color is the image?" {
		t.Fatalf("unexpected question: %v", result["question"])
	}
}

// ---------------------------------------------------------------------------
// Attachment upload: MIME rejection
// ---------------------------------------------------------------------------

func TestHandleAgentAttachmentsRejectsUnsupportedMime(t *testing.T) {
	g, _ := newGatewayWithAgentsRuntime(t, "meta-llama/Meta-Llama-3.1-8B-Instruct", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"meta-llama/Meta-Llama-3.1-8B-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"ok\"}"},"finish_reason":"stop"}]}`), nil
	}))

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("not an image")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/agent-attachments", body), auth.RoleOwner)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	g.handleAgentAttachments(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
