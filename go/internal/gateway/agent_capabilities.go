package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/agents"
)

type WebSearchRequest struct {
	Query      string
	Topic      string
	MaxResults int
}

type WebSearcher interface {
	Search(ctx context.Context, req WebSearchRequest) ([]agents.ResearchSource, error)
}

type VisionAnalyzeRequest struct {
	Attachment *agents.Attachment
	Question   string
	Focus      string
}

type VisionAnalyzer interface {
	Analyze(ctx context.Context, req VisionAnalyzeRequest) (map[string]any, error)
}

type duckDuckGoWebSearcher struct {
	client         *http.Client
	allowedDomains []string
}

func newDuckDuckGoWebSearcher() WebSearcher {
	return &duckDuckGoWebSearcher{
		client: &http.Client{Timeout: 8 * time.Second},
		allowedDomains: []string{
			"openai.com",
			"anthropic.com",
			"status.anthropic.com",
			"huggingface.co",
			"docs.vllm.ai",
			"vllm.ai",
			"docs.sglang.ai",
			"sglang.ai",
			"runpod.io",
			"docs.runpod.io",
			"vast.ai",
			"docs.vast.ai",
			"lambda.ai",
			"lambdalabs.com",
			"aws.amazon.com",
			"docs.aws.amazon.com",
			"cloud.google.com",
			"learn.microsoft.com",
			"azure.microsoft.com",
			"nvidia.com",
			"docs.nvidia.com",
		},
	}
}

func (s *duckDuckGoWebSearcher) Search(ctx context.Context, req WebSearchRequest) ([]agents.ResearchSource, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 8 {
		maxResults = 8
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", "InferaHermes/1.0 (+https://infera.ai)")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	results := parseDuckDuckGoResults(string(body), s.allowedDomains, maxResults)
	if len(results) == 0 {
		return nil, fmt.Errorf("no allowlisted official sources found")
	}
	return results, nil
}

var (
	searchResultAnchorPattern  = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	searchResultSnippetPattern = regexp.MustCompile(`(?s)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>|<div[^>]*class="result__snippet"[^>]*>(.*?)</div>`)
	htmlTagPattern             = regexp.MustCompile(`(?s)<[^>]+>`)
)

func parseDuckDuckGoResults(payload string, allowedDomains []string, maxResults int) []agents.ResearchSource {
	anchorMatches := searchResultAnchorPattern.FindAllStringSubmatch(payload, -1)
	snippetMatches := searchResultSnippetPattern.FindAllStringSubmatch(payload, -1)

	results := make([]agents.ResearchSource, 0, maxResults)
	seen := make(map[string]bool)
	for i, match := range anchorMatches {
		if len(match) < 3 {
			continue
		}
		rawURL := normalizeSearchResultURL(match[1])
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Host == "" {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if !isAllowedResearchDomain(host, allowedDomains) {
			continue
		}
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true

		title := stripHTML(match[2])
		snippet := ""
		if i < len(snippetMatches) {
			for _, candidate := range snippetMatches[i][1:] {
				candidate = stripHTML(candidate)
				if candidate != "" {
					snippet = candidate
					break
				}
			}
		}
		results = append(results, agents.ResearchSource{
			Title:   title,
			URL:     rawURL,
			Domain:  host,
			Snippet: snippet,
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results
}

func normalizeSearchResultURL(raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if redirectTarget := strings.TrimSpace(parsed.Query().Get("uddg")); redirectTarget != "" {
		if decoded, err := url.QueryUnescape(redirectTarget); err == nil {
			return decoded
		}
		return redirectTarget
	}
	return raw
}

func stripHTML(raw string) string {
	raw = html.UnescapeString(raw)
	raw = htmlTagPattern.ReplaceAllString(raw, " ")
	return strings.Join(strings.Fields(raw), " ")
}

func isAllowedResearchDomain(host string, allowedDomains []string) bool {
	for _, allowed := range allowedDomains {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

type screenshotAnalyzer struct{}

func newScreenshotAnalyzer() VisionAnalyzer {
	return &screenshotAnalyzer{}
}

func (a *screenshotAnalyzer) Analyze(ctx context.Context, req VisionAnalyzeRequest) (map[string]any, error) {
	if req.Attachment == nil {
		return nil, fmt.Errorf("attachment is required")
	}
	if strings.TrimSpace(req.Attachment.StoragePath) == "" {
		return nil, fmt.Errorf("attachment storage path is missing")
	}

	raw, err := os.ReadFile(req.Attachment.StoragePath)
	if err != nil {
		return nil, err
	}

	width := req.Attachment.Width
	height := req.Attachment.Height
	if width <= 0 || height <= 0 {
		if cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(raw)); decodeErr == nil {
			width = cfg.Width
			height = cfg.Height
		}
	}

	ocrText, ocrAvailable := extractTextWithTesseract(ctx, req.Attachment.StoragePath)
	if len(ocrText) > 4000 {
		ocrText = strings.TrimSpace(ocrText[:4000]) + "\n...[truncated]"
	}

	summary := []string{
		fmt.Sprintf("Screenshot %q (%s, %d bytes).", req.Attachment.FileName, req.Attachment.MIMEType, req.Attachment.SizeBytes),
	}
	if width > 0 && height > 0 {
		summary = append(summary, fmt.Sprintf("Image dimensions are %dx%d.", width, height))
	}
	if strings.TrimSpace(req.Focus) != "" {
		summary = append(summary, fmt.Sprintf("Requested focus: %s.", strings.TrimSpace(req.Focus)))
	}
	if strings.TrimSpace(req.Question) != "" {
		summary = append(summary, fmt.Sprintf("Requested question: %s.", strings.TrimSpace(req.Question)))
	}
	if ocrAvailable && strings.TrimSpace(ocrText) != "" {
		summary = append(summary, "OCR text was extracted for the screenshot.")
	} else if !ocrAvailable {
		summary = append(summary, "OCR is not available on this gateway, so only image metadata is available.")
	}

	return map[string]any{
		"attachment": map[string]any{
			"id":         req.Attachment.ID,
			"file_name":  req.Attachment.FileName,
			"mime_type":  req.Attachment.MIMEType,
			"size_bytes": req.Attachment.SizeBytes,
			"width":      width,
			"height":     height,
		},
		"focus":         strings.TrimSpace(req.Focus),
		"question":      strings.TrimSpace(req.Question),
		"ocr_available": ocrAvailable,
		"ocr_text":      ocrText,
		"summary":       strings.Join(summary, " "),
	}, nil
}

func extractTextWithTesseract(ctx context.Context, path string) (string, bool) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return "", false
	}
	cmd := exec.CommandContext(ctx, "tesseract", path, "stdout", "--psm", "6")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return strings.TrimSpace(string(exitErr.Stderr)), true
		}
		return "", true
	}
	return strings.TrimSpace(string(output)), true
}
