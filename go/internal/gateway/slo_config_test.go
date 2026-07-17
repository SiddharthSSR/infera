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

	wantedPanels := map[string]bool{
		"SLO v1 Availability (5m)":          false,
		"SLO v1 End-to-end p50/p95/p99 (s)": false,
		"SLO v1 TTFT p50/p95/p99 (s)":       false,
		"SLO v1 TPOT p50/p95/p99 (s)":       false,
		"SLO v1 Measurement Availability":   false,
	}
	for _, panel := range dashboard.Panels {
		if _, ok := wantedPanels[panel.Title]; !ok {
			continue
		}
		wantedPanels[panel.Title] = true
		if panel.Description == "" || panel.FieldConfig.Defaults.NoValue != "Unavailable (no data)" {
			t.Errorf("panel %q must document and render no-data explicitly", panel.Title)
		}
		for _, target := range panel.Targets {
			if !strings.Contains(target.Expr, `$model`) || !strings.Contains(target.Expr, `$routing_strategy`) {
				t.Errorf("panel %q target must apply model and routing strategy filters: %s", panel.Title, target.Expr)
			}
		}
	}
	for name, found := range wantedPanels {
		if !found {
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
