package gateway

import (
	"encoding/json"
	"io"
	"net/http"
)

const maxPublicAnalyticsBodyBytes = 4096

type publicAnalyticsEvent struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

var publicAnalyticsSchema = map[string]map[string]map[string]struct{}{
	"public_landing_view": {
		"surface": stringSet("migration_landing"),
	},
	"public_primary_cta_clicked": {
		"action":    stringSet("start_building"),
		"placement": stringSet("hero", "closing"),
	},
	"public_product_explored": {
		"product": stringSet("model_catalog", "playground", "openai_compatibility"),
		"source":  stringSet("landing", "public_navigation"),
	},
	"public_resource_opened": {
		"resource": stringSet("quickstart", "api_docs"),
		"source":   stringSet("landing", "public_navigation", "onboarding"),
	},
	"public_sign_in_intent": {
		"source": stringSet("landing", "public_navigation", "onboarding", "invitation", "sign_in_form"),
	},
	"activation_first_model_list_succeeded": {
		"surface": stringSet("onboarding", "model_catalog"),
	},
	"activation_first_unary_inference_succeeded": {
		"surface": stringSet("onboarding", "playground", "model_catalog"),
	},
	"activation_first_streaming_inference_succeeded": {
		"surface": stringSet("onboarding", "playground"),
	},
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func validatePublicAnalyticsEvent(event publicAnalyticsEvent) bool {
	propertySchema, ok := publicAnalyticsSchema[event.Name]
	if !ok || len(event.Properties) != len(propertySchema) {
		return false
	}
	for key, allowed := range propertySchema {
		value, present := event.Properties[key]
		if !present {
			return false
		}
		if _, valid := allowed[value]; !valid {
			return false
		}
	}
	return true
}

func publicAnalyticsMetricDimensions(event publicAnalyticsEvent) (source, target string) {
	switch event.Name {
	case "public_landing_view":
		return "landing", event.Properties["surface"]
	case "public_primary_cta_clicked":
		return event.Properties["placement"], event.Properties["action"]
	case "public_product_explored":
		return event.Properties["source"], event.Properties["product"]
	case "public_resource_opened":
		return event.Properties["source"], event.Properties["resource"]
	case "public_sign_in_intent":
		return event.Properties["source"], "sign_in"
	case "activation_first_model_list_succeeded",
		"activation_first_unary_inference_succeeded",
		"activation_first_streaming_inference_succeeded":
		return event.Properties["surface"], "success"
	default:
		return "", ""
	}
}

func (g *Gateway) handlePublicAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPublicAnalyticsBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var event publicAnalyticsEvent
	if err := decoder.Decode(&event); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_analytics_event", "Invalid analytics event")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		g.writeError(w, http.StatusBadRequest, "invalid_analytics_event", "Invalid analytics event")
		return
	}
	if !validatePublicAnalyticsEvent(event) {
		g.writeError(w, http.StatusBadRequest, "invalid_analytics_event", "Invalid analytics event")
		return
	}

	if g.metrics != nil {
		source, target := publicAnalyticsMetricDimensions(event)
		g.metrics.RecordPublicFunnelEvent(event.Name, source, target)
	}

	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
