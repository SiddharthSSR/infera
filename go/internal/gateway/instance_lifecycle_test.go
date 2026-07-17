package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
)

const lifecycleModel = "Qwen/Qwen2.5-7B-Instruct"

func TestInstanceLifecycleScenarios(t *testing.T) {
	tests := []struct {
		name          string
		networkReady  bool
		configure     func(t *testing.T, manager *providers.Manager, instance *providers.Instance, now *time.Time)
		wantStatus    providers.WorkerRegistrationStatus
		wantError     bool
		wantWorker    bool
		wantHeartbeat bool
	}{
		{
			name:         "provider_running_no_network",
			networkReady: false,
			wantStatus:   providers.WorkerRegistrationProviderRunningNoNetwork,
			wantError:    true,
		},
		{
			name:         "provider_running_worker_unregistered_pending",
			networkReady: true,
			wantStatus:   providers.WorkerRegistrationPending,
		},
		{
			name:         "registration_timeout",
			networkReady: true,
			configure: func(t *testing.T, _ *providers.Manager, _ *providers.Instance, now *time.Time) {
				*now = now.Add(2 * time.Minute)
			},
			wantStatus: providers.WorkerRegistrationProviderRunningUnregistered,
			wantError:  true,
		},
		{
			name:         "registered_unhealthy",
			networkReady: true,
			configure: func(t *testing.T, manager *providers.Manager, instance *providers.Instance, now *time.Time) {
				t.Helper()
				if err := manager.LinkWorker(instance.ID, "worker-inf35"); err != nil {
					t.Fatalf("LinkWorker: %v", err)
				}
				if !manager.RecordWorkerHeartbeat("worker-inf35", *now) {
					t.Fatal("expected heartbeat to be recorded")
				}
				*now = now.Add(30 * time.Second)
				if !manager.RecordWorkerUnhealthy("worker-inf35", *now) {
					t.Fatal("expected unhealthy registry state to be recorded")
				}
			},
			wantStatus:    providers.WorkerRegistrationRegisteredUnhealthy,
			wantError:     true,
			wantWorker:    true,
			wantHeartbeat: true,
		},
		{
			name:         "ready",
			networkReady: true,
			configure: func(t *testing.T, manager *providers.Manager, instance *providers.Instance, now *time.Time) {
				t.Helper()
				if err := manager.LinkWorker(instance.ID, "worker-inf35"); err != nil {
					t.Fatalf("LinkWorker: %v", err)
				}
				if !manager.RecordWorkerHeartbeat("worker-inf35", *now) {
					t.Fatal("expected heartbeat to be recorded")
				}
			},
			wantStatus:    providers.WorkerRegistrationReady,
			wantWorker:    true,
			wantHeartbeat: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
			startedAt := now
			template := &providers.Instance{
				ID:          "inst-inf35",
				ProviderID:  "provider-inf35",
				Provider:    providers.ProviderRunPod,
				Name:        test.name,
				Status:      providers.InstanceStatusRunning,
				GPUType:     providers.GPUA100_80,
				GPUCount:    1,
				Models:      []string{lifecycleModel},
				CostPerHour: 1.19,
				CreatedAt:   now,
				StartedAt:   &startedAt,
				Metadata: map[string]string{
					"api_key":                   "secret-value",
					"authorization":             "Bearer raw-secret",
					"worker_registration_token": "provider-private-payload",
				},
			}
			if !test.networkReady {
				template.Provider = providers.ProviderVastAI
			}
			if test.networkReady {
				template.PublicIP = "203.0.113.35"
				template.HTTPPort = 8081
			}

			manager, err := providers.NewManager(providers.ManagerConfig{
				DefaultProvider:           providers.ProviderMock,
				WorkerRegistrationTimeout: time.Minute,
				WorkerHeartbeatTimeout:    time.Minute,
				Now:                       func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			t.Cleanup(func() { _ = manager.Close() })
			manager.RegisterProvider(&failingProvider{provisionInstance: template})

			instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
				Name:     test.name,
				Provider: providers.ProviderMock,
				GPUType:  providers.GPUA100_80,
				GPUCount: 1,
				Models:   []string{lifecycleModel},
			})
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if test.configure != nil {
				test.configure(t, manager, instance, &now)
			}

			handler := NewInstanceHandlers(manager)
			req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/instances", nil), auth.RoleOperator)
			w := httptest.NewRecorder()
			handler.handleInstances(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("GET /api/instances: status=%d body=%s", w.Code, w.Body.String())
			}

			var response struct {
				Instances []map[string]interface{} `json:"instances"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode instances response: %v", err)
			}
			if len(response.Instances) != 1 {
				t.Fatalf("expected one instance, got %d", len(response.Instances))
			}
			got := response.Instances[0]
			if got["status"] != string(providers.InstanceStatusRunning) {
				t.Fatalf("provider status: got %v", got["status"])
			}
			wantProvider := providers.ProviderRunPod
			if !test.networkReady {
				wantProvider = providers.ProviderVastAI
			}
			if got["provider"] != string(wantProvider) {
				t.Fatalf("provider: got %v", got["provider"])
			}
			if got["worker_registration_status"] != string(test.wantStatus) {
				t.Fatalf("registration status: got %v want %q", got["worker_registration_status"], test.wantStatus)
			}
			if got["provider_network_ready"] != test.networkReady {
				t.Fatalf("network readiness: got %v want %t", got["provider_network_ready"], test.networkReady)
			}
			if (got["last_worker_registration_error"] != "") != test.wantError {
				t.Fatalf("registration error presence: got %q want=%t", got["last_worker_registration_error"], test.wantError)
			}
			if got["gpu_type"] != string(providers.GPUA100_80) || got["cost_per_hour"] != 1.19 {
				t.Fatalf("hardware/cost fields: gpu=%v cost=%v", got["gpu_type"], got["cost_per_hour"])
			}
			models, ok := got["models"].([]interface{})
			if !ok || len(models) != 1 || models[0] != lifecycleModel {
				t.Fatalf("model fields: %#v", got["models"])
			}
			if (got["worker_id"] == "worker-inf35") != test.wantWorker {
				t.Fatalf("worker ID: got %v want linked=%t", got["worker_id"], test.wantWorker)
			}
			_, hasHeartbeat := got["worker_last_heartbeat_at"]
			if hasHeartbeat != test.wantHeartbeat {
				t.Fatalf("heartbeat presence: got=%t want=%t", hasHeartbeat, test.wantHeartbeat)
			}
			for _, required := range []string{"worker_registration_deadline", "last_worker_registration_check_at"} {
				if _, ok := got[required]; !ok {
					t.Fatalf("missing lifecycle field %q", required)
				}
			}
			assertSafeLifecycleResponse(t, w.Body.String())
		})
	}
}

func TestProviderProvisionFailureLeavesNoManagedOrphan(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
		Now:             func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	manager.RegisterProvider(&failingProvider{provisionErr: &providers.ProviderError{
		Provider:   providers.ProviderMock,
		Code:       providers.ProviderErrorServiceUnavailable,
		Message:    "provider rejected provisioning request",
		StatusCode: http.StatusServiceUnavailable,
	}})

	handler := NewInstanceHandlers(manager)
	store := newTestDeploymentStore(t)
	handler.SetDeploymentStore(store)
	body, _ := json.Marshal(map[string]interface{}{
		"name":                "provider-provision-failed",
		"provider":            "mock",
		"gpu_type":            "A100_80GB",
		"gpu_count":           1,
		"models":              []string{lifecycleModel},
		"selected_model_name": lifecycleModel,
	})
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(body)), auth.RoleOperator)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.handleProvision(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
	if instances := manager.ListInstances(); len(instances) != 0 {
		t.Fatalf("expected no managed orphan, got %d instances", len(instances))
	}
	attempts, err := store.ListAttempts(auth.DefaultWorkspaceID, 10)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected one failed attempt, got %d", len(attempts))
	}
	if attempts[0].Outcome != "request_failed" || attempts[0].InstanceID != "" {
		t.Fatalf("unexpected failed attempt: outcome=%q instance=%q", attempts[0].Outcome, attempts[0].InstanceID)
	}
	if attempts[0].FailureReason != "provider rejected provisioning request" {
		t.Fatalf("unexpected safe failure reason %q", attempts[0].FailureReason)
	}
	assertSafeLifecycleResponse(t, w.Body.String()+attempts[0].FailureReason)
}

func assertSafeLifecycleResponse(t *testing.T, payload string) {
	t.Helper()
	lower := strings.ToLower(payload)
	for _, forbidden := range []string{
		"api_key",
		"authorization",
		"provider_credentials",
		"worker_registration_token",
		"secret-value",
		"raw-secret",
		"provider-private-payload",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("lifecycle response exposed forbidden content %q", forbidden)
		}
	}
}
