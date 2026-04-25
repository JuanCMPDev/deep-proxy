package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
)

func writeError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openai.ErrorResponse{
		Error: openai.ErrorDetail{
			Type:    errType,
			Message: message,
		},
	})
}
