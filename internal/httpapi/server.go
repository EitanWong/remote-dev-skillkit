package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
	"github.com/EitanWong/remote-dev-skillkit/internal/wsproto"
)

type Server struct {
	Gateway      *gateway.MemoryGateway
	StatePath    string
	StateStore   gateway.StateStore
	OperatorAuth *operatorauth.Authorizer
	stateMu      *sync.Mutex
}

func NewServer(gw *gateway.MemoryGateway) Server {
	return Server{Gateway: gw, stateMu: &sync.Mutex{}}
}

func NewServerWithState(gw *gateway.MemoryGateway, statePath string) Server {
	if strings.TrimSpace(statePath) == "" {
		return NewServer(gw)
	}
	store, _ := gateway.NewFileStateStore(statePath)
	server := NewServerWithStateStore(gw, store)
	server.StatePath = statePath
	return server
}

func NewServerWithStateStore(gw *gateway.MemoryGateway, store gateway.StateStore) Server {
	return Server{Gateway: gw, StateStore: store, stateMu: &sync.Mutex{}}
}

func NewServerWithOperatorAuth(gw *gateway.MemoryGateway, statePath string, auth *operatorauth.Authorizer) Server {
	if strings.TrimSpace(statePath) == "" {
		return NewServerWithOperatorAuthAndStateStore(gw, nil, auth)
	}
	store, _ := gateway.NewFileStateStore(statePath)
	server := NewServerWithOperatorAuthAndStateStore(gw, store, auth)
	server.StatePath = statePath
	return server
}

func NewServerWithOperatorAuthAndStateStore(gw *gateway.MemoryGateway, store gateway.StateStore, auth *operatorauth.Authorizer) Server {
	return Server{Gateway: gw, StateStore: store, OperatorAuth: auth, stateMu: &sync.Mutex{}}
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/trust", s.trust)
	mux.HandleFunc("GET /v1/trust-bundle", s.getTrustBundle)
	mux.HandleFunc("GET /v1/enrollment/revocations", s.getEnrollmentRevocations)
	mux.HandleFunc("POST /v1/enrollment/certificates", s.issueEnrollmentCertificate)
	mux.HandleFunc("POST /v1/enrollment/certificates/renew", s.renewEnrollmentCertificate)
	mux.HandleFunc("POST /v1/trust-bundle", s.updateTrustBundle)
	mux.HandleFunc("POST /v1/tickets", s.createTicket)
	mux.HandleFunc("GET /v1/tickets/", s.ticketSubresource)
	mux.HandleFunc("GET /join/", s.join)
	mux.HandleFunc("GET /v1/hosts", s.listHosts)
	mux.HandleFunc("POST /v1/hosts/register", s.registerHost)
	mux.HandleFunc("GET /v1/hosts/", s.hostSubresource)
	mux.HandleFunc("GET /v1/ws/hosts/", s.hostWebSocket)
	mux.HandleFunc("POST /v1/hosts/", s.hostAction)
	mux.HandleFunc("POST /v1/jobs", s.createJob)
	mux.HandleFunc("GET /v1/jobs/", s.getJob)
	mux.HandleFunc("POST /v1/jobs/", s.jobAction)
	mux.HandleFunc("GET /v1/artifacts/", s.getArtifact)
	mux.HandleFunc("GET /v1/audit", s.listAudit)
	return mux
}

func (s Server) hostWebSocket(w http.ResponseWriter, r *http.Request) {
	hostID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/ws/hosts/"), "/")
	if hostID == "" {
		writeError(w, http.StatusNotFound, "unknown websocket host endpoint")
		return
	}
	conn, err := wsproto.Upgrade(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer conn.Close()
	for {
		job, ok, err := s.nextJobForHost(r.Context(), hostID, 60*time.Second)
		if err != nil {
			_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
			return
		}
		if !ok {
			if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageNoop, HostID: hostID}); err != nil {
				return
			}
			continue
		}
		if !s.persistStateNoResponse() {
			_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
			return
		}
		if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageJob, HostID: hostID, JobID: job.ID, Job: &job}); err != nil {
			return
		}
	responseLoop:
		for {
			var msg wsproto.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.HostID == "" {
				msg.HostID = hostID
			}
			if msg.JobID == "" {
				msg.JobID = job.ID
			}
			if msg.HostID != hostID || msg.JobID != job.ID {
				_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "websocket job response host or job mismatch"})
				return
			}
			switch msg.Type {
			case wsproto.MessageComplete:
				if _, _, err := s.Gateway.CompleteJobForHost(hostID, job.ID, msg.ArtifactContent); err != nil {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
					return
				}
				if !s.persistStateNoResponse() {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
					return
				}
				if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageComplete, HostID: hostID, JobID: job.ID}); err != nil {
					return
				}
				break responseLoop
			case wsproto.MessageFail:
				if _, _, err := s.Gateway.FailJobForHostWithArtifact(hostID, job.ID, msg.Reason, msg.ArtifactContent); err != nil {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
					return
				}
				if !s.persistStateNoResponse() {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
					return
				}
				if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageFail, HostID: hostID, JobID: job.ID}); err != nil {
					return
				}
				break responseLoop
			case wsproto.MessageArtifact:
				if _, _, err := s.Gateway.AppendJobArtifactForHost(hostID, job.ID, msg.ArtifactContent); err != nil {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: err.Error()})
					return
				}
				if !s.persistStateNoResponse() {
					_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "persist gateway state failed"})
					return
				}
				if err := conn.WriteJSON(wsproto.Message{Type: wsproto.MessageArtifact, HostID: hostID, JobID: job.ID}); err != nil {
					return
				}
			default:
				_ = conn.WriteJSON(wsproto.Message{Type: wsproto.MessageError, Error: "unsupported websocket message type"})
				return
			}
		}
	}
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s Server) trust(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"trust": s.Gateway.TrustBundle()})
}

func (s Server) getTrustBundle(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"trust_bundle": s.Gateway.SignedTrustBundle()})
}

func (s Server) getEnrollmentRevocations(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "operator issuer role is required")
		return
	}
	revocations, ok := s.Gateway.EnrollmentRevocations()
	if !ok {
		writeError(w, http.StatusNotFound, "enrollment revocations not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revocations": revocations})
}

func (s Server) issueEnrollmentCertificate(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "operator issuer role is required")
		return
	}
	var req struct {
		TicketCode          string   `json:"ticket_code"`
		Name                string   `json:"name"`
		OS                  string   `json:"os"`
		Arch                string   `json:"arch"`
		Capabilities        []string `json:"capabilities"`
		IdentityKeyID       string   `json:"identity_key_id"`
		IdentityPublicKey   string   `json:"identity_public_key"`
		IdentityFingerprint string   `json:"identity_fingerprint"`
		ValidMinutes        int      `json:"valid_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ValidMinutes == 0 {
		req.ValidMinutes = 60
	}
	certificate, err := s.Gateway.IssueEnrollmentCertificate(gateway.EnrollmentCertificateRequest{
		TicketCode:          req.TicketCode,
		Name:                req.Name,
		OS:                  req.OS,
		Arch:                req.Arch,
		Capabilities:        req.Capabilities,
		IdentityKeyID:       req.IdentityKeyID,
		IdentityPublicKey:   req.IdentityPublicKey,
		IdentityFingerprint: req.IdentityFingerprint,
		ValidMinutes:        req.ValidMinutes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	root, ok := s.Gateway.EnrollmentRoot()
	if !ok {
		writeError(w, http.StatusInternalServerError, "enrollment root not configured")
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"certificate":             certificate,
		"certificate_fingerprint": fingerprint,
		"enrollment_root":         root,
	})
}

func (s Server) renewEnrollmentCertificate(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeEnrollmentIssuer(r) {
		writeError(w, http.StatusUnauthorized, "operator issuer role is required")
		return
	}
	var req struct {
		Certificate  model.HostEnrollmentCertificate `json:"certificate"`
		ValidMinutes int                             `json:"valid_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ValidMinutes == 0 {
		req.ValidMinutes = 60
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(req.Certificate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	certificate, err := s.Gateway.RenewEnrollmentCertificate(gateway.EnrollmentCertificateRenewalRequest{
		Certificate:  req.Certificate,
		ValidMinutes: req.ValidMinutes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	root, ok := s.Gateway.EnrollmentRoot()
	if !ok {
		writeError(w, http.StatusInternalServerError, "enrollment root not configured")
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"certificate":                      certificate,
		"certificate_fingerprint":          fingerprint,
		"previous_certificate_fingerprint": previousFingerprint,
		"enrollment_root":                  root,
	})
}

func (s Server) authorizeEnrollmentIssuer(r *http.Request) bool {
	return s.authorizeOperator(r, operatorauth.RoleIssuer)
}

func (s Server) updateTrustBundle(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	var req struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	bundle, err := s.Gateway.UpdateSignedTrustBundle(req.TrustBundle)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trust_bundle": bundle})
}

func (s Server) createTicket(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
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
	if !s.persistState(w) {
		return
	}
	manifestRoot := manifestRootPublicKey(s.Gateway.ManifestRoot())
	writeJSON(w, http.StatusCreated, map[string]any{
		"ticket":                ticket,
		"joinUrl":               requestBaseURL(r) + "/join/" + ticket.Code,
		"manifestUrl":           requestBaseURL(r) + "/v1/tickets/" + ticket.Code + "/manifest",
		"manifestRootPublicKey": manifestRoot,
	})
}

func (s Server) ticketSubresource(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitTicketSubresource(r.URL.Path)
	if !ok || resource != "manifest" {
		writeError(w, http.StatusNotFound, "unknown ticket endpoint")
		return
	}
	manifest, err := s.Gateway.JoinManifest(code, requestBaseURL(r), requestBaseURL(r)+"/join/"+code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":              manifest,
		"manifestRootPublicKey": manifestRootPublicKey(s.Gateway.ManifestRoot()),
	})
}

func (s Server) join(w http.ResponseWriter, r *http.Request) {
	code, resource, ok := splitJoinPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown join endpoint")
		return
	}
	manifestURL := requestBaseURL(r) + "/v1/tickets/" + code + "/manifest"
	manifestRoot := manifestRootPublicKey(s.Gateway.ManifestRoot())
	if _, err := s.Gateway.JoinManifest(code, requestBaseURL(r), requestBaseURL(r)+"/join/"+code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch resource {
	case "":
		s.joinPage(w, r, code, manifestURL)
	case "bootstrap.sh":
		writeShellBootstrap(w, manifestURL, manifestRoot)
	case "bootstrap.ps1":
		writePowerShellBootstrap(w, manifestURL, manifestRoot)
	default:
		writeError(w, http.StatusNotFound, "unknown join resource")
	}
}

func (s Server) joinPage(w http.ResponseWriter, r *http.Request, code, manifestURL string) {
	joinBase := strings.TrimRight(requestBaseURL(r), "/") + "/join/" + code
	shellCommand := "curl -fsSL " + shellQuote(joinBase+"/bootstrap.sh") + " | sh"
	powerShellCommand := "powershell -NoProfile -ExecutionPolicy Bypass -Command \"irm '" + powerShellSingleQuoteValue(joinBase+"/bootstrap.ps1") + "' | iex\""
	locale := joinLocale(r)
	copy := joinCopy(locale)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="%s">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 2rem; line-height: 1.45; max-width: 860px; }
    code, pre { background: #f4f4f5; border-radius: 6px; padding: .2rem .35rem; }
    pre { padding: 1rem; overflow-x: auto; }
    .note { border-left: 4px solid #2563eb; padding-left: 1rem; color: #1f2937; }
  </style>
</head>
<body>
  <h1>%s</h1>
  <p class="note">%s</p>
  <h2>macOS / Linux</h2>
  <pre><code>%s</code></pre>
  <h2>Windows PowerShell</h2>
  <pre><code>%s</code></pre>
  <h2>%s</h2>
  <ol>
    <li>%s</li>
    <li>%s</li>
    <li>%s</li>
  </ol>
  <p>Manifest: <code>%s</code></p>
</body>
</html>`,
		html.EscapeString(locale),
		html.EscapeString(copy.Title),
		html.EscapeString(copy.Heading),
		html.EscapeString(copy.Note),
		html.EscapeString(shellCommand),
		html.EscapeString(powerShellCommand),
		html.EscapeString(copy.NextHeading),
		copy.StepCheck,
		copy.StepStart,
		copy.StepAgent,
		html.EscapeString(manifestURL),
	)
}

type joinPageCopy struct {
	Title       string
	Heading     string
	Note        string
	NextHeading string
	StepCheck   string
	StepStart   string
	StepAgent   string
}

func joinCopy(locale string) joinPageCopy {
	switch locale {
	case "zh-CN":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit 连接",
			Heading:     "连接这台机器",
			Note:        "在需要帮助的电脑上运行一条命令。连接是可见、仅出站、可撤销，并且限定在此支持工单内。",
			NextHeading: "接下来会发生什么",
			StepCheck:   `启动脚本会检查 <code>rdev</code>。`,
			StepStart:   `它会用 <code>--transport auto</code> 启动一个可见的协助式主机会话。`,
			StepAgent:   "Agent 会等待主机上线，在策略需要时完成批准，然后运行受限的修复任务。",
		}
	case "es":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Conectar Esta Maquina",
			Note:        "Ejecuta un comando en el equipo que necesita ayuda. La conexion es visible, solo saliente, revocable y limitada a este ticket.",
			NextHeading: "Que pasa despues",
			StepCheck:   `El bootstrap comprueba <code>rdev</code>.`,
			StepStart:   `Inicia una sesion visible con <code>--transport auto</code>.`,
			StepAgent:   "El Agent espera el host, lo aprueba si la politica lo requiere y ejecuta trabajos de reparacion limitados.",
		}
	case "fr":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connecter Cette Machine",
			Note:        "Executez une commande sur l'ordinateur a aider. La connexion est visible, sortante uniquement, revocable et limitee a ce ticket.",
			NextHeading: "Et ensuite",
			StepCheck:   `Le bootstrap verifie <code>rdev</code>.`,
			StepStart:   `Il demarre une session visible avec <code>--transport auto</code>.`,
			StepAgent:   "L'Agent attend le host, l'approuve si la politique l'exige, puis execute des reparations limitees.",
		}
	case "de":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Diese Maschine Verbinden",
			Note:        "Fuhre einen Befehl auf dem Computer aus, der Hilfe braucht. Die Verbindung ist sichtbar, nur ausgehend, widerrufbar und auf dieses Ticket begrenzt.",
			NextHeading: "Was als Nachstes passiert",
			StepCheck:   `Der Bootstrap pruft <code>rdev</code>.`,
			StepStart:   `Er startet eine sichtbare Sitzung mit <code>--transport auto</code>.`,
			StepAgent:   "Der Agent wartet auf den Host, genehmigt ihn falls erforderlich und startet begrenzte Reparaturjobs.",
		}
	case "ja":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "このマシンを接続",
			Note:        "サポートが必要なコンピューターで 1 つのコマンドを実行します。接続は可視、アウトバウンドのみ、取り消し可能で、このサポートチケットに限定されます。",
			NextHeading: "次に行われること",
			StepCheck:   `bootstrap は <code>rdev</code> を確認します。`,
			StepStart:   `<code>--transport auto</code> で可視のホストセッションを開始します。`,
			StepAgent:   "Agent はホストを待ち、ポリシーが必要とする場合に承認し、限定された修復ジョブを実行します。",
		}
	case "ko":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "이 머신 연결",
			Note:        "도움이 필요한 컴퓨터에서 명령 하나를 실행합니다. 연결은 보이는 방식이며, 아웃바운드 전용이고, 철회 가능하며, 이 지원 티켓 범위로 제한됩니다.",
			NextHeading: "다음 단계",
			StepCheck:   `bootstrap 이 <code>rdev</code> 를 확인합니다.`,
			StepStart:   `<code>--transport auto</code> 로 보이는 호스트 세션을 시작합니다.`,
			StepAgent:   "Agent 는 호스트를 기다리고, 정책상 필요하면 승인한 뒤 제한된 복구 작업을 실행합니다.",
		}
	case "pt-BR":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Conectar Esta Maquina",
			Note:        "Execute um comando no computador que precisa de ajuda. A conexao e visivel, somente de saida, revogavel e limitada a este ticket.",
			NextHeading: "O que acontece depois",
			StepCheck:   `O bootstrap verifica <code>rdev</code>.`,
			StepStart:   `Ele inicia uma sessao visivel com <code>--transport auto</code>.`,
			StepAgent:   "O Agent aguarda o host, aprova quando a politica exige e executa tarefas de reparo limitadas.",
		}
	case "hi":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "इस मशीन को कनेक्ट करें",
			Note:        "जिस कंप्यूटर को मदद चाहिए उस पर एक कमांड चलाएं। कनेक्शन दिखने वाला, केवल outbound, revoke करने योग्य, और इस support ticket तक सीमित है।",
			NextHeading: "आगे क्या होगा",
			StepCheck:   `bootstrap <code>rdev</code> जांचता है।`,
			StepStart:   `यह <code>--transport auto</code> के साथ visible host session शुरू करता है।`,
			StepAgent:   "Agent host का इंतजार करता है, policy की जरूरत पर approve करता है, और scoped repair jobs चलाता है।",
		}
	case "ar":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "توصيل هذا الجهاز",
			Note:        "شغّل أمرا واحدا على الكمبيوتر الذي يحتاج إلى مساعدة. الاتصال ظاهر، صادر فقط، قابل للإلغاء، ومحدود بتذكرة الدعم هذه.",
			NextHeading: "ماذا يحدث بعد ذلك",
			StepCheck:   `يتحقق bootstrap من <code>rdev</code>.`,
			StepStart:   `يبدأ جلسة host مرئية باستخدام <code>--transport auto</code>.`,
			StepAgent:   "ينتظر Agent ظهور host، ويوافق عليه عند الحاجة حسب السياسة، ثم يشغل مهام إصلاح محددة النطاق.",
		}
	case "ru":
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Подключить Эту Машину",
			Note:        "Выполните одну команду на компьютере, которому нужна помощь. Подключение видимое, только исходящее, отзывное и ограничено этим тикетом.",
			NextHeading: "Что будет дальше",
			StepCheck:   `bootstrap проверит <code>rdev</code>.`,
			StepStart:   `Он запустит видимую сессию host с <code>--transport auto</code>.`,
			StepAgent:   "Agent дождется host, выполнит approval при необходимости и запустит ограниченные repair jobs.",
		}
	default:
		return joinPageCopy{
			Title:       "Remote Dev Skillkit Join",
			Heading:     "Connect This Machine",
			Note:        "Run one command on the computer that needs help. The connection is visible, outbound-only, revocable, and scoped to this support ticket.",
			NextHeading: "What Happens Next",
			StepCheck:   `The bootstrap checks for <code>rdev</code>.`,
			StepStart:   `It starts a visible attended host session with <code>--transport auto</code>.`,
			StepAgent:   "The Agent waits for the host, approves it when policy requires, and runs scoped repair jobs.",
		}
	}
}

func joinLocale(r *http.Request) string {
	if lang := supportedJoinLocale(r.URL.Query().Get("lang")); lang != "" {
		return lang
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if lang := supportedJoinLocale(tag); lang != "" {
			return lang
		}
	}
	return "en"
}

func supportedJoinLocale(tag string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	if normalized == "" {
		return ""
	}
	lower := strings.ToLower(normalized)
	switch lower {
	case "en":
		return "en"
	case "zh-cn", "zh-hans", "zh":
		return "zh-CN"
	case "es":
		return "es"
	case "fr":
		return "fr"
	case "de":
		return "de"
	case "ja":
		return "ja"
	case "ko":
		return "ko"
	case "pt-br", "pt":
		return "pt-BR"
	case "hi":
		return "hi"
	case "ar":
		return "ar"
	case "ru":
		return "ru"
	default:
		if base, _, ok := strings.Cut(lower, "-"); ok {
			return supportedJoinLocale(base)
		}
		return ""
	}
}

func writeShellBootstrap(w http.ResponseWriter, manifestURL, manifestRootPublicKey string) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	rootArg := ""
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		rootArg = " --manifest-root-public-key " + shellQuote(manifestRootPublicKey)
	}
	_, _ = fmt.Fprintf(w, `#!/bin/sh
set -eu
if ! command -v rdev >/dev/null 2>&1; then
  echo "rdev is required. Install the verified rdev release package, then run this bootstrap again." >&2
  exit 127
fi
echo "Starting visible Remote Dev Skillkit host session..."
exec rdev host serve --manifest-url %s%s --transport auto --once=false
`, shellQuote(manifestURL), rootArg)
}

func writePowerShellBootstrap(w http.ResponseWriter, manifestURL, manifestRootPublicKey string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	rootArg := ""
	if strings.TrimSpace(manifestRootPublicKey) != "" {
		rootArg = " --manifest-root-public-key '" + powerShellSingleQuoteValue(manifestRootPublicKey) + "'"
	}
	_, _ = fmt.Fprintf(w, `$ErrorActionPreference = 'Stop'
if (-not (Get-Command rdev -ErrorAction SilentlyContinue)) {
  Write-Error "rdev is required. Install the verified rdev release package, then run this bootstrap again."
  exit 127
}
Write-Host "Starting visible Remote Dev Skillkit host session..."
& rdev host serve --manifest-url '%s'%s --transport auto --once=false
`, powerShellSingleQuoteValue(manifestURL), rootArg)
}

func manifestRootPublicKey(root model.TrustBundle) string {
	if root.SigningKeyID == "" || root.PublicKey == "" {
		return ""
	}
	return root.SigningKeyID + ":" + root.PublicKey
}

func (s Server) listHosts(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
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
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"host": host})
}

func (s Server) hostAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
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
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
	case "revoke":
		var req struct {
			Reason string `json:"reason"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
		host, err := s.Gateway.RevokeHost(hostID, req.Reason)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"host": host})
	default:
		writeError(w, http.StatusNotFound, "unknown host action")
	}
}

func (s Server) hostSubresource(w http.ResponseWriter, r *http.Request) {
	if hostID, ok := splitHostID(r.URL.Path); ok {
		if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
			writeError(w, http.StatusForbidden, "auditor role is required")
			return
		}
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
		wait, err := parseLongPollWait(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job, ok, err := s.nextJobForHost(r.Context(), hostID, wait)
		if err != nil {
			if err == context.Canceled {
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"job": nil})
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	case resource == "trust-bundle" && action == "update":
		currentSequence, err := parseOptionalInt(r.URL.Query().Get("current_sequence"), "current_sequence")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		update, err := s.Gateway.TrustBundleUpdateForHost(hostID, currentSequence, r.URL.Query().Get("current_hash"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"trust_bundle_update": update})
	default:
		writeError(w, http.StatusNotFound, "unknown host subresource")
	}
}

func (s Server) nextJobForHost(ctx context.Context, hostID string, wait time.Duration) (model.Job, bool, error) {
	if wait <= 0 {
		return s.Gateway.NextJobForHost(hostID)
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, ok, err := s.Gateway.NextJobForHost(hostID)
		if err != nil || ok {
			return job, ok, err
		}
		select {
		case <-ctx.Done():
			return model.Job{}, false, ctx.Err()
		case <-deadline.C:
			return model.Job{}, false, nil
		case <-ticker.C:
		}
	}
}

func (s Server) createJob(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
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
	if !s.persistState(w) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": job})
}

func (s Server) getJob(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	if jobID, resource, ok := splitJobSubresource(r.URL.Path); ok {
		switch resource {
		case "artifacts":
			writeJSON(w, http.StatusOK, map[string]any{"artifacts": s.Gateway.Artifacts(jobID)})
		case "evidence-bundle":
			s.exportJobEvidenceBundle(w, r, jobID)
		default:
			writeError(w, http.StatusNotFound, "unknown job subresource")
		}
		return
	}
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

func (s Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	artifactID, ok := splitArtifactID(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown artifact endpoint")
		return
	}
	artifact, err := s.Gateway.Artifact(artifactID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}

func (s Server) exportJobEvidenceBundle(w http.ResponseWriter, r *http.Request, jobID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "operator role is required")
		return
	}
	out := r.URL.Query().Get("out")
	if out == "" {
		writeError(w, http.StatusBadRequest, "out query parameter is required")
		return
	}
	job, err := s.Gateway.Job(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	manifest, err := evidence.ExportDirectory(out, evidence.Input{
		Job:         job,
		Artifacts:   s.Gateway.Artifacts(jobID),
		AuditEvents: s.Gateway.AuditEvents(),
		GeneratedAt: time.Now(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"out":               out,
		"job_id":            manifest.JobID,
		"file_count":        len(manifest.Files) + 1,
		"audit_event_count": manifest.AuditEventCount,
		"audit_root_hash":   manifest.AuditRootHash,
		"manifest":          manifest,
	})
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
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifact": artifact})
	case "fail":
		var req struct {
			HostID          string `json:"host_id"`
			Reason          string `json:"reason"`
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
		job, artifact, err := s.Gateway.FailJobForHostWithArtifact(req.HostID, jobID, req.Reason, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		payload := map[string]any{"job": job}
		if artifact != nil {
			payload["artifact"] = artifact
		}
		writeJSON(w, http.StatusOK, payload)
	case "artifact":
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
		job, artifact, err := s.Gateway.AppendJobArtifactForHost(req.HostID, jobID, req.ArtifactContent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.persistState(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifact": artifact})
	default:
		writeError(w, http.StatusNotFound, "unknown job action")
	}
}

func (s Server) listAudit(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r, operatorauth.RoleAuditor, operatorauth.RoleOperator) {
		writeError(w, http.StatusForbidden, "auditor role is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": s.Gateway.AuditEvents(),
	})
}

func (s Server) authorizeOperator(r *http.Request, roles ...string) bool {
	if !s.OperatorAuth.Enabled() {
		return true
	}
	return s.OperatorAuth.AuthorizeBearer(r.Header.Get("Authorization"), roles...)
}

func (s Server) persistState(w http.ResponseWriter) bool {
	if err := s.persistStateInternal(); err != nil {
		writeError(w, http.StatusInternalServerError, "persist gateway state: "+err.Error())
		return false
	}
	return true
}

func (s Server) persistStateNoResponse() bool {
	return s.persistStateInternal() == nil
}

func (s Server) persistStateInternal() error {
	if s.StateStore == nil {
		if strings.TrimSpace(s.StatePath) == "" {
			return nil
		}
		store, err := gateway.NewFileStateStore(s.StatePath)
		if err != nil {
			return fmt.Errorf("configure gateway state store: %w", err)
		}
		s.StateStore = store
	}
	if s.StateStore == nil {
		return nil
	}
	if s.stateMu != nil {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
	}
	if _, err := s.StateStore.SaveFrom(s.Gateway); err != nil {
		return err
	}
	return nil
}

func parseLongPollWait(r *http.Request) (time.Duration, error) {
	query := r.URL.Query()
	if raw := query.Get("wait_ms"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 60000 {
			return 0, fmt.Errorf("wait_ms must be between 0 and 60000")
		}
		return time.Duration(value) * time.Millisecond, nil
	}
	if raw := query.Get("wait_seconds"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 60 {
			return 0, fmt.Errorf("wait_seconds must be between 0 and 60")
		}
		return time.Duration(value) * time.Second, nil
	}
	return 0, nil
}

func parseOptionalInt(raw, name string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
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

func splitTicketSubresource(path string) (code string, resource string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/tickets/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitJoinPath(path string) (code string, resource string, ok bool) {
	rest := strings.TrimPrefix(path, "/join/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], "", true
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	return "", "", false
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>*?()[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func powerShellSingleQuoteValue(value string) string {
	return strings.ReplaceAll(value, "'", "''")
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

func splitJobSubresource(path string) (jobID string, resource string, ok bool) {
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

func splitArtifactID(path string) (artifactID string, ok bool) {
	rest := strings.TrimPrefix(path, "/v1/artifacts/")
	if rest == path {
		return "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}
