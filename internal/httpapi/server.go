package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type Server struct {
	Gateway *gateway.MemoryGateway
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw}
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/tickets", s.createTicket)
	mux.HandleFunc("GET /v1/hosts", s.listHosts)
	mux.HandleFunc("GET /v1/audit", s.listAudit)
	return mux
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s Server) createTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode         model.HostMode `json:"mode"`
		TTLSeconds   int            `json:"ttl_seconds"`
		Capabilities []string       `json:"capabilities"`
		Reason       string         `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Mode == "" {
		req.Mode = model.HostModeAttendedTemporary
	}
	if req.TTLSeconds == 0 {
		req.TTLSeconds = 7200
	}
	if req.Reason == "" {
		req.Reason = "remote support"
	}
	ticket, err := s.Gateway.CreateTicket(req.Mode, req.TTLSeconds, req.Capabilities, req.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ticket":  ticket,
		"joinUrl": "https://agent.lunflux.com/join/" + ticket.Code,
	})
}

func (s Server) listHosts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"hosts": s.Gateway.Hosts(r.URL.Query().Get("status")),
	})
}

func (s Server) listAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"events": s.Gateway.AuditEvents(),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
