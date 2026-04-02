package gateway

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infera/infera/go/internal/auth"
)

func TestParseDuckDuckGoResultsFiltersToAllowlistedDomains(t *testing.T) {
	html := `
<div class="result">
  <a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fstatus.runpod.io%2F">RunPod Status</a>
  <div class="result__snippet">Official status page.</div>
</div>
<div class="result">
  <a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Frandom-blog.example%2Fincident">Random Blog</a>
  <div class="result__snippet">Unofficial commentary.</div>
</div>`

	results := parseDuckDuckGoResults(html, []string{"status.runpod.io"}, 5)
	if len(results) != 1 {
		t.Fatalf("expected one allowlisted result, got %+v", results)
	}
	if results[0].Domain != "status.runpod.io" {
		t.Fatalf("expected allowlisted domain, got %+v", results[0])
	}
}

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
