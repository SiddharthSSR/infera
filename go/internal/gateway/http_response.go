package gateway

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}

func (g *Gateway) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	writeJSON(w, status, data)
}

func (g *Gateway) writeError(w http.ResponseWriter, status int, errType, message string) {
	writeError(w, status, errType, message)
}
