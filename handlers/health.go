package handlers

import (
	"net/http"
)

// HealthCheck is a simple health check endpoint
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
