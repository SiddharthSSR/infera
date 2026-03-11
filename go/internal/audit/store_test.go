package audit

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAppendInference(t *testing.T) {
	s := newTestStore(t)

	err := s.AppendInference(InferenceAuditRecord{
		Timestamp:    time.Now().UTC(),
		RequestID:    "req_1",
		KeyID:        "inf_abcd1234",
		Model:        "meta-llama/Llama-3.1-8B-Instruct",
		WorkerID:     "w1",
		Stream:       false,
		MessageCount: 2,
		TokenCount:   128,
		PromptHash:   "aabbccddeeff0011",
		Status:       "success",
		LatencyMS:    320,
	})
	if err != nil {
		t.Fatalf("AppendInference: %v", err)
	}
}

func TestUsageByKey(t *testing.T) {
	s := newTestStore(t)

	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	records := []InferenceAuditRecord{
		{
			Timestamp:  base.Add(-2 * time.Hour),
			RequestID:  "req_1",
			KeyID:      "inf_key_a",
			Model:      "m1",
			Status:     "success",
			TokenCount: 100,
		},
		{
			Timestamp:  base.Add(-1 * time.Hour),
			RequestID:  "req_2",
			KeyID:      "inf_key_a",
			Model:      "m1",
			Status:     "success",
			TokenCount: 50,
		},
		{
			Timestamp:  base.Add(-30 * time.Minute),
			RequestID:  "req_3",
			KeyID:      "inf_key_b",
			Model:      "m1",
			Status:     "inference_error",
			TokenCount: 10,
		},
	}
	for _, rec := range records {
		if err := s.AppendInference(rec); err != nil {
			t.Fatalf("AppendInference: %v", err)
		}
	}

	rows, err := s.UsageByKey(UsageQuery{
		Start:  base.Add(-3 * time.Hour),
		End:    base.Add(time.Hour),
		Bucket: "day",
	})
	if err != nil {
		t.Fatalf("UsageByKey: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 usage rows (2 keys), got %d", len(rows))
	}

	for _, row := range rows {
		switch row.KeyID {
		case "inf_key_a":
			if row.RequestCount != 2 {
				t.Fatalf("expected 2 requests for key a, got %d", row.RequestCount)
			}
			if row.TokenCount != 150 {
				t.Fatalf("expected 150 tokens for key a, got %d", row.TokenCount)
			}
			if row.SuccessCount != 2 || row.ErrorCount != 0 {
				t.Fatalf("expected success=2 error=0 for key a, got success=%d error=%d", row.SuccessCount, row.ErrorCount)
			}
		case "inf_key_b":
			if row.RequestCount != 1 {
				t.Fatalf("expected 1 request for key b, got %d", row.RequestCount)
			}
			if row.TokenCount != 10 {
				t.Fatalf("expected 10 tokens for key b, got %d", row.TokenCount)
			}
			if row.SuccessCount != 0 || row.ErrorCount != 1 {
				t.Fatalf("expected success=0 error=1 for key b, got success=%d error=%d", row.SuccessCount, row.ErrorCount)
			}
		default:
			t.Fatalf("unexpected key id %q", row.KeyID)
		}
	}
}
