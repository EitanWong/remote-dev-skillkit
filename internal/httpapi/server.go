package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

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
	mux.HandleFunc("POST /v1/hosts/register", s.registerHost)
	mux.HandleFunc("GET /v1/hosts/", s.hostSubresource)
	mux.HandleFunc("POST /v1/hosts/", s.hostAction)
	mux.HandleFunc("POST /v1/jobs", s.createJob)
	mux.HandleFunc("GET /v1/jobs/", s.getJob)
	mux.HandleFunc("POST /v1/jobs/", s.jobAction)
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

func (s Server) registerHost(w http.ResponseWriter, r *http.Request) {
	var req model.HostRegistration
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	host, err := s.Gateway.RegisterHost(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"host": host})
}

func (s Server) hostAction(w http.ResponseWriter, r *http.Request) {
	hostID, action, ok := splitHostAction(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown host endpoint")
		return
	}
	switch action {
	case "approve":
		var req struct {
			Capabilities []string `json:"capabilities"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
		host, err := s.Gateway.ApproveHost(hostID, req.Capabilities)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
	default:
		writeError(w, http.StatusNotFound, "unknown host action")
	}
}

func (s Server) hostSubresource(w http.ResponseWriter, r *http.Request) {
	if hostID, ok := splitHostID(r.URL.Path); ok {
		host, err := s.Gateway.Host(hostID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
		return
	}
	hostID, resource, action, ok := splitHostSubresource(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown host endpoint")
		return
	}
	switch {
	case resource == "jobs" && action == "next":
		job, ok, err := s.Gateway.NextJobForHost(hostID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"job": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	default:
		writeError(w, http.StatusNotFound, "unknown host subresource")
	}
}

func (s Server) createJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HostID  string         `json:"host_id"`
		Adapter string         `json:"adapter"`
		Intent  string         `json:"intent"`
		Policy  map[string]any `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	job, err := s.Gateway.CreateJob(req.HostID, req.Adapter, req.Intent, req.Policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": job})
}

func (s Server) getJob(w http.ResponseWriter, r *http.Request) {
	jobID, ok := splitJobID(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown job endpoint")
		return
	}
	job, err := s.Gateway.Job(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s Server) jobAction(w http.ResponseWriter, r *http.Request) {
	jobID, action, ok := splitJobAction(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown job endpoint")
		return
	}
	switch action {
	case "complete":
		var req struct {
			HostID          string `json:"host_id"`
			ArtifactContent string `json:"artifact_content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.HostID == "" {
			writeError(w, http.StatusBadRequest, "host_id is required")
			return
		}
		job, artifact, err := s.Gateway.CompleteJobForHost(req.HostID, jobID, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifact": artifact})
	default:
		writeError(w, http.StatusNotFound, "unknown job action")
	}
}

func (s Server) listAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"events": s.Gateway.AuditEvents(),
	})
}

func splitHostAction(path string) (hostID string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/hosts/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitHostID(path string) (hostID string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/hosts/")
	if rest == path {
		return "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func splitHostSubresource(path string) (hostID string, resource string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/hosts/")
	if rest == path {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func splitJobID(path string) (jobID string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/jobs/")
	if rest == path {
		return "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func splitJobAction(path string) (jobID string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/jobs/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
