package httpx

import (
	"encoding/json"
	"net/http"
)

type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, title, detail, requestID string) {
	WriteJSON(w, status, Problem{Type: "about:blank", Title: title, Status: status, Detail: detail, RequestID: requestID})
}
