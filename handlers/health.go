package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthCheck is a simple health check endpoint
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(response)
}
