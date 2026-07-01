package mcpstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

const protocolVersion = "2025-11-25"

type Server struct {
	Gateway *gateway.MemoryGateway
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := encoder.Encode(errorResponse(nil, -32700, "parse error")); err != nil {
				return err
			}
			continue
		}
		resp := s.handle(req)
		if req.ID == nil {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s Server) handle(req request) response {
	switch req.Method {
	case "initialize":
		return success(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo": map[string]any{
				"name":    "rdev-mcp",
				"version": buildinfo.Version,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		})
	case "notifications/initialized":
		return success(req.ID, map[string]any{})
	case "tools/list":
		return success(req.ID, map[string]any{"tools": contracts.Tools()})
	case "tools/call":
		result, err := s.callTool(req.Params)
		if err != nil {
			return errorResponse(req.ID, -32000, err.Error())
		}
		return success(req.ID, result)
	default:
		return errorResponse(req.ID, -32601, "method not found")
	}
}

func (s Server) callTool(raw json.RawMessage) (result map[string]any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("invalid tool arguments: %v", recovered)
		}
	}()
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	var data any
	switch params.Name {
	case "rdev.invites.create":
		data, err = s.createInvite(params.Arguments)
	case "rdev.tickets.create":
		data, err = s.createTicket(params.Arguments)
	case "rdev.tickets.revoke":
		data, err = s.revokeTicket(params.Arguments)
	case "rdev.hosts.list":
		data, err = s.listHosts(params.Arguments)
	case "rdev.hosts.capabilities":
		data, err = s.hostCapabilities(params.Arguments)
	case "rdev.hosts.approve":
		data, err = s.approveHost(params.Arguments)
	case "rdev.hosts.revoke":
		data, err = s.revokeHost(params.Arguments)
	case "rdev.jobs.create":
		data, err = s.createJob(params.Arguments)
	case "rdev.jobs.status":
		data, err = s.jobStatus(params.Arguments)
	case "rdev.jobs.cancel":
		data, err = s.cancelJob(params.Arguments)
	case "rdev.jobs.approve":
		data, err = s.approveJob(params.Arguments)
	case "rdev.artifacts.list":
		data, err = s.listArtifacts(params.Arguments)
	case "rdev.artifacts.read":
		data, err = s.readArtifact(params.Arguments)
	case "rdev.audit.query":
		data, err = s.queryAudit(params.Arguments)
	case "rdev.policy.explain":
		data, err = s.explainPolicy(params.Arguments)
	case "rdev.policy.explain_shell":
		data, err = s.explainShellPolicy(params.Arguments)
	case "rdev.enrollment.verify_certificate":
		data, err = s.verifyEnrollmentCertificate(params.Arguments)
	case "rdev.adapter.verify_result":
		data, err = s.verifyAdapterResult(params.Arguments)
	case "rdev.adapter.verify_lifecycle":
		data, err = s.verifyAdapterLifecycle(params.Arguments)
	case "rdev.adapter.verify_cancellation":
		data, err = s.verifyAdapterCancellation(params.Arguments)
	case "rdev.adapter.verify_runtime":
		data, err = s.verifyAdapterRuntime(params.Arguments)
	default:
		err = fmt.Errorf("unknown tool %q", params.Name)
	}
	if err != nil {
		return nil, err
	}
	return toolResult(data)
}

func (s Server) createInvite(args map[string]any) (any, error) {
	mode := model.HostMode(stringArg(args, "mode", string(model.HostModeAttendedTemporary)))
	ttl := intArg(args, "ttl_seconds", 7200)
	reason := stringArg(args, "reason", "remote support")
	capabilities := stringSliceArg(args, "capabilities")
	ticket, err := s.Gateway.CreateTicket(mode, ttl, capabilities, reason)
	if err != nil {
		return nil, err
	}
	gatewayURL := requiredString(args, "gateway_url")
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:          gatewayURL,
		Ticket:              ticket,
		Transport:           stringArg(args, "transport", "auto"),
		Once:                boolArg(args, "once", false),
		RequireHostApproval: boolArg(args, "require_host_approval", true),
		RdevCommand:         stringArg(args, "rdev_command", "rdev"),
	})
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s Server) createTicket(args map[string]any) (any, error) {
	mode := model.HostMode(stringArg(args, "mode", string(model.HostModeAttendedTemporary)))
	ttl := intArg(args, "ttl_seconds", 7200)
	reason := stringArg(args, "reason", "remote support")
	capabilities := stringSliceArg(args, "capabilities")
	ticket, err := s.Gateway.CreateTicket(mode, ttl, capabilities, reason)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ticket":  ticket,
		"joinUrl": publicExampleJoinURL(ticket.Code),
	}, nil
}

func publicExampleJoinURL(ticketCode string) string {
	return "https://agent.example.com/join/" + ticketCode
}

func (s Server) revokeTicket(args map[string]any) (any, error) {
	return s.Gateway.RevokeTicket(requiredString(args, "ticket_id"), stringArg(args, "reason", ""))
}

func (s Server) listHosts(args map[string]any) (any, error) {
	return map[string]any{"hosts": s.Gateway.Hosts(stringArg(args, "status", ""))}, nil
}

func (s Server) hostCapabilities(args map[string]any) (any, error) {
	host, err := s.Gateway.Host(requiredString(args, "host_id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"host_id":      host.ID,
		"status":       host.Status,
		"capabilities": host.Capabilities,
	}, nil
}

func (s Server) approveHost(args map[string]any) (any, error) {
	return s.Gateway.ApproveHost(requiredString(args, "host_id"), stringSliceArg(args, "capabilities"))
}

func (s Server) revokeHost(args map[string]any) (any, error) {
	return s.Gateway.RevokeHost(requiredString(args, "host_id"), stringArg(args, "reason", ""))
}

func (s Server) createJob(args map[string]any) (any, error) {
	return s.Gateway.CreateJob(
		requiredString(args, "host_id"),
		requiredString(args, "adapter"),
		requiredString(args, "intent"),
		objectArg(args, "policy"),
	)
}

func (s Server) jobStatus(args map[string]any) (any, error) {
	return s.Gateway.Job(requiredString(args, "job_id"))
}

func (s Server) cancelJob(args map[string]any) (any, error) {
	return s.Gateway.CancelJob(requiredString(args, "job_id"), stringArg(args, "reason", ""))
}

func (s Server) approveJob(args map[string]any) (any, error) {
	return s.Gateway.ApproveJob(
		requiredString(args, "job_id"),
		requiredString(args, "approval_id"),
		requiredString(args, "decision"),
		stringArg(args, "reason", ""),
	)
}

func (s Server) listArtifacts(args map[string]any) (any, error) {
	return map[string]any{"artifacts": s.Gateway.Artifacts(requiredString(args, "job_id"))}, nil
}

func (s Server) readArtifact(args map[string]any) (any, error) {
	return s.Gateway.Artifact(requiredString(args, "artifact_id"))
}

func (s Server) queryAudit(args map[string]any) (any, error) {
	targetID := stringArg(args, "target_id", "")
	limit := intArg(args, "limit", 100)
	events := s.Gateway.AuditEvents()
	filtered := make([]model.AuditEvent, 0, len(events))
	for _, event := range events {
		if targetID == "" || event.TargetID == targetID {
			filtered = append(filtered, event)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return map[string]any{"events": filtered}, nil
}

func (s Server) explainPolicy(args map[string]any) (any, error) {
	return policy.Explain(
		model.HostMode(requiredString(args, "mode")),
		policy.Capability(requiredString(args, "capability")),
	), nil
}

func (s Server) explainShellPolicy(args map[string]any) (any, error) {
	return policy.ExplainShellJob(
		model.HostMode(requiredString(args, "mode")),
		objectArg(args, "policy"),
	), nil
}

const EnrollmentCertificateVerificationSchemaVersion = "rdev.enrollment-certificate-verification.v1"

type enrollmentCertificateVerificationReport struct {
	SchemaVersion              string         `json:"schema_version"`
	OK                         bool           `json:"ok"`
	CertificateSchema          string         `json:"certificate_schema,omitempty"`
	CertificateFingerprint     string         `json:"certificate_fingerprint,omitempty"`
	RevocationListSchema       string         `json:"revocation_list_schema,omitempty"`
	RevokedCertificateCount    int            `json:"revoked_certificate_count,omitempty"`
	IssuerKeyID                string         `json:"issuer_key_id,omitempty"`
	RootKeyID                  string         `json:"root_key_id,omitempty"`
	SubjectIdentityFingerprint string         `json:"subject_identity_fingerprint,omitempty"`
	TicketCode                 string         `json:"ticket_code,omitempty"`
	Mode                       model.HostMode `json:"mode,omitempty"`
	NotBefore                  *time.Time     `json:"not_before,omitempty"`
	NotAfter                   *time.Time     `json:"not_after,omitempty"`
	VerifiedAt                 time.Time      `json:"verified_at"`
	Checks                     []string       `json:"checks"`
	Errors                     []string       `json:"errors,omitempty"`
	RecommendedActions         []string       `json:"recommended_actions,omitempty"`
}

func (s Server) verifyEnrollmentCertificate(args map[string]any) (any, error) {
	certificateJSON := stringArg(args, "certificate_json", "")
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		certificateJSON = artifact.Content
	}
	if certificateJSON == "" {
		return nil, fmt.Errorf("certificate_json or artifact_id is required")
	}
	rootPublicKey := requiredString(args, "root_public_key")
	verifyAt := time.Now().UTC()
	if value := stringArg(args, "verify_at", ""); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("verify_at must be RFC3339: %w", err)
		}
		verifyAt = parsed.UTC()
	}
	report := enrollmentCertificateVerificationReport{
		SchemaVersion: EnrollmentCertificateVerificationSchemaVersion,
		VerifiedAt:    verifyAt,
		Checks:        []string{},
	}
	certificate, err := decodeEnrollmentCertificateJSON([]byte(certificateJSON))
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		report.RecommendedActions = append(report.RecommendedActions, "provide a JSON object using schema rdev.host-enrollment-certificate.v1 or a wrapper with certificate/enrollment_certificate")
		return report, nil
	}
	report.CertificateSchema = certificate.SchemaVersion
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report, nil
	}
	report.CertificateFingerprint = fingerprint
	report.IssuerKeyID = certificate.IssuerKeyID
	report.SubjectIdentityFingerprint = certificate.SubjectIdentityFingerprint
	report.TicketCode = certificate.TicketCode
	report.Mode = certificate.Mode
	notBefore := certificate.NotBefore.UTC()
	notAfter := certificate.NotAfter.UTC()
	report.NotBefore = &notBefore
	report.NotAfter = &notAfter
	report.Checks = append(report.Checks, "certificate_json_decoded")
	root, err := trustref.Parse(rootPublicKey)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		report.RecommendedActions = append(report.RecommendedActions, "pin the enrollment root as key_id:base64url_ed25519_public_key")
		return report, nil
	}
	report.RootKeyID = root.SigningKeyID
	report.Checks = append(report.Checks, "root_public_key_decoded")
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, verifyAt); err != nil {
		report.Errors = append(report.Errors, err.Error())
		report.RecommendedActions = append(report.RecommendedActions, "reject this host registration until a valid enrollment certificate is presented")
		return report, nil
	}
	report.Checks = append(report.Checks, "signature_valid", "validity_window_active", "issuer_matches_root")
	if revocationsJSON := stringArg(args, "revocations_json", ""); revocationsJSON != "" || stringArg(args, "revocations_artifact_id", "") != "" {
		if artifactID := stringArg(args, "revocations_artifact_id", ""); artifactID != "" {
			artifact, err := s.Gateway.Artifact(artifactID)
			if err != nil {
				return nil, err
			}
			revocationsJSON = artifact.Content
		}
		revocations, err := decodeEnrollmentRevocationListJSON([]byte(revocationsJSON))
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.RecommendedActions = append(report.RecommendedActions, "provide a JSON object using schema rdev.host-enrollment-revocations.v1")
			return report, nil
		}
		report.RevocationListSchema = revocations.SchemaVersion
		report.RevokedCertificateCount = len(revocations.RevokedCertificates)
		report.Checks = append(report.Checks, "revocation_list_json_decoded")
		if err := model.VerifyHostEnrollmentRevocationListSignature(revocations, root, verifyAt); err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.RecommendedActions = append(report.RecommendedActions, "refresh the enrollment revocation list from the trusted authority before trusting this registration")
			return report, nil
		}
		report.Checks = append(report.Checks, "revocation_list_signature_valid", "revocation_list_fresh")
		if err := model.VerifyHostEnrollmentCertificateNotRevoked(certificate, revocations); err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.RecommendedActions = append(report.RecommendedActions, "reject this host registration because its enrollment certificate is revoked")
			return report, nil
		}
		report.Checks = append(report.Checks, "certificate_not_revoked")
	}
	report.OK = true
	return report, nil
}

func decodeEnrollmentCertificateJSON(content []byte) (model.HostEnrollmentCertificate, error) {
	var certificate model.HostEnrollmentCertificate
	if err := json.Unmarshal(content, &certificate); err == nil && certificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion {
		return certificate, nil
	}
	var wrapped struct {
		Certificate           model.HostEnrollmentCertificate `json:"certificate"`
		EnrollmentCertificate model.HostEnrollmentCertificate `json:"enrollment_certificate"`
	}
	if err := json.Unmarshal(content, &wrapped); err != nil {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("decode enrollment certificate JSON: %w", err)
	}
	switch {
	case wrapped.Certificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion:
		return wrapped.Certificate, nil
	case wrapped.EnrollmentCertificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion:
		return wrapped.EnrollmentCertificate, nil
	default:
		return model.HostEnrollmentCertificate{}, fmt.Errorf("unsupported enrollment certificate schema")
	}
}

func decodeEnrollmentRevocationListJSON(content []byte) (model.HostEnrollmentRevocationList, error) {
	var list model.HostEnrollmentRevocationList
	if err := json.Unmarshal(content, &list); err != nil {
		return model.HostEnrollmentRevocationList{}, fmt.Errorf("decode enrollment revocation list JSON: %w", err)
	}
	if list.SchemaVersion != model.HostEnrollmentRevocationListSchemaVersion {
		return model.HostEnrollmentRevocationList{}, fmt.Errorf("unsupported enrollment revocation list schema %q", list.SchemaVersion)
	}
	return list, nil
}

func (s Server) verifyAdapterResult(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
	}
	requiredFields := stringSliceArg(args, "required_string_fields")
	if len(requiredFields) == 0 {
		requiredFields = []string{"workspace_root"}
	}
	return adapterkit.VerifyResultArtifactJSON([]byte(artifactJSON), adapterkit.ResultArtifactContract{
		Adapter:                 requiredString(args, "adapter"),
		SchemaVersion:           requiredString(args, "schema"),
		CommandFields:           stringSliceArg(args, "command_fields"),
		RequiredStringFields:    requiredFields,
		RequireTiming:           boolArg(args, "require_timing", true),
		RequireRedaction:        boolArg(args, "require_redaction", true),
		RejectUnredactedSecrets: boolArg(args, "reject_secret_patterns", true),
	}), nil
}

func (s Server) verifyAdapterLifecycle(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
	}
	schema := stringArg(args, "schema", adapterkit.LifecycleManifestSchemaVersion)
	return adapterkit.VerifyLifecycleManifestJSON([]byte(artifactJSON), adapterkit.LifecycleContract{
		Adapter:                 requiredString(args, "adapter"),
		SchemaVersion:           schema,
		RequiredPhases:          stringSliceArg(args, "required_phases"),
		RequireSafety:           boolArg(args, "require_safety", true),
		RequireCancellation:     boolArg(args, "require_cancellation", true),
		RequireResultSchema:     boolArg(args, "require_result_schema", true),
		RejectUnredactedSecrets: boolArg(args, "reject_secret_patterns", true),
	}), nil
}

func (s Server) verifyAdapterCancellation(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
	}
	requiredFields := stringSliceArg(args, "required_string_fields")
	if len(requiredFields) == 0 {
		requiredFields = []string{"workspace_root"}
	}
	return adapterkit.VerifyCancellationArtifactJSON([]byte(artifactJSON), adapterkit.CancellationContract{
		Adapter:                 requiredString(args, "adapter"),
		SchemaVersion:           requiredString(args, "schema"),
		CommandFields:           stringSliceArg(args, "command_fields"),
		RequiredStringFields:    requiredFields,
		RequireTiming:           boolArg(args, "require_timing", true),
		RequireRedaction:        boolArg(args, "require_redaction", true),
		RejectUnredactedSecrets: boolArg(args, "reject_secret_patterns", true),
	}), nil
}

func (s Server) verifyAdapterRuntime(args map[string]any) (any, error) {
	artifactJSON := stringArg(args, "artifact_json", "")
	if artifactID := stringArg(args, "artifact_id", ""); artifactID != "" {
		artifact, err := s.Gateway.Artifact(artifactID)
		if err != nil {
			return nil, err
		}
		artifactJSON = artifact.Content
	}
	if artifactJSON == "" {
		return nil, fmt.Errorf("artifact_json or artifact_id is required")
	}
	return adapterkit.VerifyRuntimeFixtureJSON([]byte(artifactJSON), adapterkit.RuntimeFixtureContract{
		Adapter:               requiredString(args, "adapter"),
		RequiredPhases:        stringSliceArg(args, "required_phases"),
		RequireSuccessful:     boolArg(args, "require_successful", true),
		RequireCleanup:        boolArg(args, "require_cleanup", true),
		RequireResultArtifact: boolArg(args, "require_result_artifact", false),
		RequireCancellation:   boolArg(args, "require_cancellation", false),
	}), nil
}

func success(id any, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id any, code int, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func toolResult(data any) (map[string]any, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(bytes)},
		},
		"structuredContent": data,
	}, nil
}

func requiredString(args map[string]any, key string) string {
	value := stringArg(args, key, "")
	if value == "" {
		panic(fmt.Sprintf("missing required argument %q", key))
	}
	return value
}

func stringArg(args map[string]any, key, fallback string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func intArg(args map[string]any, key string, fallback int) int {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return fallback
	}
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}

func stringSliceArg(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && text != "" {
			values = append(values, text)
		}
	}
	return values
}

func objectArg(args map[string]any, key string) map[string]any {
	value, ok := args[key]
	if !ok || value == nil {
		return map[string]any{}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return object
}
