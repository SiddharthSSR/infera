package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type prometheusRuleFile struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Record string            `yaml:"record"`
			Alert  string            `yaml:"alert"`
			Expr   string            `yaml:"expr"`
			For    string            `yaml:"for"`
			Labels map[string]string `yaml:"labels"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

func TestSLOPrometheusRulesContract(t *testing.T) {
	data := readRepositoryFile(t, "deploy", "observability", "prometheus", "rules", "infera-slo-v1.yml")
	var config prometheusRuleFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse SLO rules: %v", err)
	}

	records := map[string]string{}
	alerts := map[string]struct {
		expr        string
		forDuration string
	}{}
	for _, group := range config.Groups {
		for _, rule := range group.Rules {
			if rule.Record != "" {
				records[rule.Record] = rule.Expr
			}
			if rule.Alert != "" {
				alerts[rule.Alert] = struct {
					expr        string
					forDuration string
				}{rule.Expr, rule.For}
			}
			lower := strings.ToLower(rule.Expr)
			for _, forbidden := range []string{"tenant", "workspace", "api_key", "request_id", "worker_id", "secret"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("rule %q exposes forbidden high-cardinality/private dimension %q", rule.Record+rule.Alert, forbidden)
				}
			}
		}
	}

	for _, name := range []string{
		"infera:slo_v1:request_rate5m", "infera:slo_v1:success_rate5m", "infera:slo_v1:error_rate5m",
		"infera:slo_v1:e2e_seconds_p50_5m", "infera:slo_v1:e2e_seconds_p95_5m", "infera:slo_v1:e2e_seconds_p99_5m",
		"infera:slo_v1:ttft_seconds_p50_5m", "infera:slo_v1:ttft_seconds_p95_5m", "infera:slo_v1:ttft_seconds_p99_5m",
		"infera:slo_v1:tpot_seconds_p50_5m", "infera:slo_v1:tpot_seconds_p95_5m", "infera:slo_v1:tpot_seconds_p99_5m",
		"infera:slo_v1:measurement_rate5m",
		"infera:slo_v1:request_count14d", "infera:slo_v1:success_count14d", "infera:slo_v1:availability_ratio14d",
		"infera:slo_v1:e2e_seconds_p95_14d", "infera:slo_v1:e2e_good_ratio14d",
		"infera:slo_v1:ttft_seconds_p95_30m", "infera:slo_v1:ttft_seconds_p95_14d", "infera:slo_v1:ttft_good_ratio14d", "infera:slo_v1:ttft_sample_rate5m",
		"infera:slo_v1:tpot_seconds_p95_30m", "infera:slo_v1:tpot_seconds_p95_14d", "infera:slo_v1:tpot_good_ratio14d", "infera:slo_v1:tpot_sample_rate5m",
		"infera:slo_v1:measurement_count14d",
	} {
		if records[name] == "" {
			t.Errorf("missing required recording rule %q", name)
		}
	}
	for name, wantFor := range map[string]string{
		"InferaSLOAvailabilityFastBurn": "5m",
		"InferaSLOAvailabilitySlowBurn": "30m",
	} {
		alert, ok := alerts[name]
		if !ok {
			t.Errorf("missing required alert %q", name)
			continue
		}
		if alert.forDuration != wantFor {
			t.Errorf("alert %q for=%q, want %q", name, alert.forDuration, wantFor)
		}
		if !strings.Contains(alert.expr, "request_rate5m > 0.01") {
			t.Errorf("alert %q must explicitly suppress no-traffic pages", name)
		}
	}
	for name, metric := range map[string]string{
		"InferaSLOTTFTSustainedHigh": "ttft",
		"InferaSLOTPOTSustainedHigh": "tpot",
	} {
		alert, ok := alerts[name]
		if !ok {
			t.Errorf("missing required SLO-v1 latency alert %q", name)
			continue
		}
		if alert.forDuration != "10m" {
			t.Errorf("alert %q for=%q, want 10m", name, alert.forDuration)
		}
		for _, want := range []string{
			"infera:slo_v1:" + metric + "_seconds_p95_5m",
			"infera:slo_v1:" + metric + "_seconds_p95_30m",
			"infera:slo_v1:" + metric + "_sample_rate5m",
			"and on (model, routing_strategy, measurement)",
		} {
			if !strings.Contains(alert.expr, want) {
				t.Errorf("alert %q must contain %q", name, want)
			}
		}
	}

	legacyAlerts := string(readRepositoryFile(t, "deploy", "observability", "prometheus", "rules", "infera-alerts.yml"))
	for _, removed := range []string{"InferaInferenceTTFTHigh", "InferaInferenceTPOTHigh"} {
		if strings.Contains(legacyAlerts, removed) {
			t.Errorf("legacy latency alert %q must be removed", removed)
		}
	}
}

func TestSLOGrafanaDashboardContract(t *testing.T) {
	data := readRepositoryFile(t, "deploy", "observability", "grafana", "dashboards", "infera-overview.json")
	var dashboard struct {
		Panels []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			FieldConfig struct {
				Defaults struct {
					NoValue string `json:"noValue"`
				} `json:"defaults"`
			} `json:"fieldConfig"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
		Templating struct {
			List []struct {
				Name string `json:"name"`
			} `json:"list"`
		} `json:"templating"`
	}
	if err := json.Unmarshal(data, &dashboard); err != nil {
		t.Fatalf("parse Grafana dashboard: %v", err)
	}

	variables := map[string]bool{}
	for _, variable := range dashboard.Templating.List {
		variables[variable.Name] = true
	}
	for _, name := range []string{"model", "routing_strategy"} {
		if !variables[name] {
			t.Errorf("missing dashboard variable %q", name)
		}
	}

	wantedPanels := map[string][]string{
		"SLO v1 Availability Attainment (14d)":        {"availability_ratio14d"},
		"SLO v1 End-to-end Operational + 14d p95 (s)": {"e2e_seconds_p95_14d"},
		"SLO v1 TTFT Operational + 14d p95 (s)":       {"ttft_seconds_p95_14d"},
		"SLO v1 TPOT Operational + 14d p95 (s)":       {"tpot_seconds_p95_14d"},
		"SLO v1 Measurement Availability (14d)":       {"measurement_count14d"},
		"SLO v1 Latency Objective Attainment (14d)":   {"e2e_good_ratio14d", "ttft_good_ratio14d", "tpot_good_ratio14d"},
	}
	foundPanels := map[string]bool{}
	for _, panel := range dashboard.Panels {
		wantedQueries, ok := wantedPanels[panel.Title]
		if !ok {
			continue
		}
		foundPanels[panel.Title] = true
		if panel.Description == "" || panel.FieldConfig.Defaults.NoValue != "Unavailable (no data)" {
			t.Errorf("panel %q must document and render no-data explicitly", panel.Title)
		}
		allExpressions := ""
		for _, target := range panel.Targets {
			allExpressions += "\n" + target.Expr
			if !strings.Contains(target.Expr, `$model`) || !strings.Contains(target.Expr, `$routing_strategy`) {
				t.Errorf("panel %q target must apply model and routing strategy filters: %s", panel.Title, target.Expr)
			}
		}
		for _, want := range wantedQueries {
			if !strings.Contains(allExpressions, want) {
				t.Errorf("panel %q must query %q", panel.Title, want)
			}
		}
	}
	for name := range wantedPanels {
		if !foundPanels[name] {
			t.Errorf("missing dashboard panel %q", name)
		}
	}
}

func readRepositoryFile(t *testing.T, path ...string) []byte {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(append([]string{root}, path...)...))
	if err != nil {
		t.Fatalf("read repository file: %v", err)
	}
	return data
}
