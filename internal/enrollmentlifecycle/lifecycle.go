package enrollmentlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	KeyCustodySchemaVersion     = "rdev.enrollment-key-custody.v1"
	FleetRenewalSchemaVersion   = "rdev.enrollment-fleet-renewal-plan.v1"
	EmergencyDrillSchemaVersion = "rdev.enrollment-emergency-drill.v1"
)

type KeyCustodyRecord struct {
	SchemaVersion        string    `json:"schema_version"`
	KeyID                string    `json:"key_id"`
	RootPublicKey        string    `json:"root_public_key"`
	PublicKeyFingerprint string    `json:"public_key_fingerprint"`
	Custodian            string    `json:"custodian"`
	CustodyProvider      string    `json:"custody_provider"`
	RotationDays         int       `json:"rotation_days"`
	DualControl          bool      `json:"dual_control"`
	BreakGlassRequired   bool      `json:"break_glass_required"`
	CreatedAt            time.Time `json:"created_at"`
	ReviewAfter          time.Time `json:"review_after"`
}

type FleetRenewalPolicy struct {
	RootPublicKey      string
	RenewBefore        time.Duration
	RenewValidFor      time.Duration
	MaximumSkew        time.Duration
	RequireRevocations bool
}

type FleetRenewalPlan struct {
	SchemaVersion      string                 `json:"schema_version"`
	GeneratedAt        time.Time              `json:"generated_at"`
	RootPublicKey      string                 `json:"root_public_key"`
	RenewBeforeSeconds int64                  `json:"renew_before_seconds"`
	RenewValidSeconds  int64                  `json:"renew_valid_seconds"`
	MaximumSkewSeconds int64                  `json:"maximum_skew_seconds"`
	RequireRevocations bool                   `json:"require_revocations"`
	CertificateCount   int                    `json:"certificate_count"`
	RenewalDueCount    int                    `json:"renewal_due_count"`
	ExpiredCount       int                    `json:"expired_count"`
	RevokedCount       int                    `json:"revoked_count"`
	Items              []FleetRenewalPlanItem `json:"items"`
}

type FleetRenewalPlanItem struct {
	CertificateFingerprint string    `json:"certificate_fingerprint"`
	HostName               string    `json:"host_name"`
	OS                     string    `json:"os"`
	Arch                   string    `json:"arch"`
	Mode                   string    `json:"mode"`
	NotAfter               time.Time `json:"not_after"`
	RenewalDue             bool      `json:"renewal_due"`
	Expired                bool      `json:"expired"`
	Revoked                bool      `json:"revoked"`
	Reason                 string    `json:"reason,omitempty"`
}

type EmergencyDrill struct {
	SchemaVersion       string       `json:"schema_version"`
	GeneratedAt         time.Time    `json:"generated_at"`
	Name                string       `json:"name"`
	Scenario            string       `json:"scenario"`
	OperatorRole        string       `json:"operator_role"`
	RootPublicKey       string       `json:"root_public_key"`
	RevocationsPath     string       `json:"revocations_path,omitempty"`
	RevokedCertificates int          `json:"revoked_certificates"`
	Checklist           []DrillCheck `json:"checklist"`
	Passed              bool         `json:"passed"`
}

type DrillCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func NewKeyCustodyRecord(root model.TrustBundle, custodian, provider string, rotationDays int, dualControl, breakGlass bool, now time.Time) (KeyCustodyRecord, error) {
	if root.SigningKeyID == "" || root.PublicKey == "" {
		return KeyCustodyRecord{}, fmt.Errorf("root public key is required")
	}
	if strings.TrimSpace(custodian) == "" {
		return KeyCustodyRecord{}, fmt.Errorf("custodian is required")
	}
	if strings.TrimSpace(provider) == "" {
		return KeyCustodyRecord{}, fmt.Errorf("custody provider is required")
	}
	if rotationDays <= 0 {
		return KeyCustodyRecord{}, fmt.Errorf("rotation days must be positive")
	}
	fingerprint, err := root.Fingerprint()
	if err != nil {
		return KeyCustodyRecord{}, err
	}
	now = now.UTC()
	return KeyCustodyRecord{
		SchemaVersion:        KeyCustodySchemaVersion,
		KeyID:                root.SigningKeyID,
		RootPublicKey:        root.SigningKeyID + ":" + root.PublicKey,
		PublicKeyFingerprint: fingerprint,
		Custodian:            strings.TrimSpace(custodian),
		CustodyProvider:      strings.TrimSpace(provider),
		RotationDays:         rotationDays,
		DualControl:          dualControl,
		BreakGlassRequired:   breakGlass,
		CreatedAt:            now,
		ReviewAfter:          now.Add(time.Duration(rotationDays) * 24 * time.Hour),
	}, nil
}

func BuildFleetRenewalPlan(certificates []model.HostEnrollmentCertificate, revocations *model.HostEnrollmentRevocationList, policy FleetRenewalPolicy, now time.Time) (FleetRenewalPlan, error) {
	if strings.TrimSpace(policy.RootPublicKey) == "" {
		return FleetRenewalPlan{}, fmt.Errorf("root public key is required")
	}
	if policy.RenewBefore <= 0 {
		return FleetRenewalPlan{}, fmt.Errorf("renew before must be positive")
	}
	if policy.RenewValidFor <= 0 {
		return FleetRenewalPlan{}, fmt.Errorf("renew valid for must be positive")
	}
	if policy.MaximumSkew < 0 {
		return FleetRenewalPlan{}, fmt.Errorf("maximum skew must not be negative")
	}
	if policy.RequireRevocations && revocations == nil {
		return FleetRenewalPlan{}, fmt.Errorf("revocations are required by policy")
	}
	now = now.UTC()
	revoked := map[string]bool{}
	if revocations != nil {
		for _, entry := range revocations.RevokedCertificates {
			revoked[entry.CertificateFingerprint] = true
		}
	}
	items := make([]FleetRenewalPlanItem, 0, len(certificates))
	for _, certificate := range certificates {
		fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
		if err != nil {
			return FleetRenewalPlan{}, err
		}
		expired := !now.Before(certificate.NotAfter.UTC().Add(policy.MaximumSkew))
		renewalDue := expired || !now.Add(policy.RenewBefore).Before(certificate.NotAfter.UTC())
		item := FleetRenewalPlanItem{
			CertificateFingerprint: fingerprint,
			HostName:               certificate.HostName,
			OS:                     certificate.OS,
			Arch:                   certificate.Arch,
			Mode:                   string(certificate.Mode),
			NotAfter:               certificate.NotAfter.UTC(),
			RenewalDue:             renewalDue,
			Expired:                expired,
			Revoked:                revoked[fingerprint],
		}
		switch {
		case item.Revoked:
			item.Reason = "certificate is revoked"
		case item.Expired:
			item.Reason = "certificate is expired"
		case item.RenewalDue:
			item.Reason = "certificate is within renewal window"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RenewalDue != items[j].RenewalDue {
			return items[i].RenewalDue
		}
		return items[i].NotAfter.Before(items[j].NotAfter)
	})
	plan := FleetRenewalPlan{
		SchemaVersion:      FleetRenewalSchemaVersion,
		GeneratedAt:        now,
		RootPublicKey:      policy.RootPublicKey,
		RenewBeforeSeconds: int64(policy.RenewBefore.Seconds()),
		RenewValidSeconds:  int64(policy.RenewValidFor.Seconds()),
		MaximumSkewSeconds: int64(policy.MaximumSkew.Seconds()),
		RequireRevocations: policy.RequireRevocations,
		CertificateCount:   len(items),
		Items:              items,
	}
	for _, item := range items {
		if item.RenewalDue {
			plan.RenewalDueCount++
		}
		if item.Expired {
			plan.ExpiredCount++
		}
		if item.Revoked {
			plan.RevokedCount++
		}
	}
	return plan, nil
}

func NewEmergencyDrill(name, scenario, operatorRole, rootPublicKey, revocationsPath string, revocations *model.HostEnrollmentRevocationList, now time.Time) EmergencyDrill {
	checks := []DrillCheck{
		{Name: "name", Passed: strings.TrimSpace(name) != "", Detail: "drill name is set"},
		{Name: "scenario", Passed: strings.TrimSpace(scenario) != "", Detail: "scenario is set"},
		{Name: "operator_role", Passed: strings.TrimSpace(operatorRole) != "", Detail: "operator role is set"},
		{Name: "root_public_key", Passed: strings.TrimSpace(rootPublicKey) != "", Detail: "root public key is pinned"},
		{Name: "revocations", Passed: revocations != nil, Detail: "signed revocation list is available"},
	}
	revoked := 0
	if revocations != nil {
		revoked = len(revocations.RevokedCertificates)
		checks = append(checks, DrillCheck{Name: "revocation_signature", Passed: revocations.Signature != "", Detail: "revocation list is signed"})
	}
	passed := true
	for _, check := range checks {
		if !check.Passed {
			passed = false
		}
	}
	return EmergencyDrill{
		SchemaVersion:       EmergencyDrillSchemaVersion,
		GeneratedAt:         now.UTC(),
		Name:                strings.TrimSpace(name),
		Scenario:            strings.TrimSpace(scenario),
		OperatorRole:        strings.TrimSpace(operatorRole),
		RootPublicKey:       strings.TrimSpace(rootPublicKey),
		RevocationsPath:     sanitizeReference(revocationsPath),
		RevokedCertificates: revoked,
		Checklist:           checks,
		Passed:              passed,
	}
}

func ReadCertificateSet(path string) ([]model.HostEnrollmentCertificate, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var direct []model.HostEnrollmentCertificate
	if err := json.Unmarshal(content, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Certificates []model.HostEnrollmentCertificate `json:"certificates"`
	}
	if err := json.Unmarshal(content, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Certificates, nil
}

func WriteJSON(path string, value any, force bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("out is required")
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func sanitizeReference(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return parsed.Scheme + "://example.com"
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
