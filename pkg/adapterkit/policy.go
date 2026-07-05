package adapterkit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PolicyPlanSchemaVersion is the schema version for PolicyPlan.
const PolicyPlanSchemaVersion = "rdev.adapter-policy-plan.v1"

// PolicyPlan is the structured output of the plan phase for a third-party
// adapter. It declares what external effects the adapter will have, which
// approvals are required before the prepare/run phases can proceed, and what
// workspace boundaries constrain the run.
//
// Operators and agents review the PolicyPlan before approving execution. The
// plan is also embedded in the RuntimeFixture so reviewers can trace what was
// promised during planning versus what actually happened during execution.
type PolicyPlan struct {
	SchemaVersion string `json:"schema_version"`
	Adapter       string `json:"adapter"`
	JobID         string `json:"job_id,omitempty"`
	// ExternalConsequences lists every external side effect this adapter may
	// cause outside the workspace (network calls, file writes outside scope,
	// service restarts, etc.). An empty list is valid only if the adapter is
	// genuinely read-only within the workspace.
	ExternalConsequences []string `json:"external_consequences"`
	// RequiredApprovals lists the operator actions that must be pre-approved
	// before execution. Each entry is a human-readable sentence. Any entry
	// triggers an approval gate before the prepare phase begins.
	RequiredApprovals []string `json:"required_approvals"`
	// WorkspaceBoundaries lists the paths (or globs) the adapter may write to.
	// An empty list means the adapter declares no write scope; the host kernel
	// will enforce the default workspace root boundary.
	WorkspaceBoundaries []string `json:"workspace_boundaries,omitempty"`
	// DeclaredSideEffects lists any additional side effects that are not
	// captured by ExternalConsequences but are relevant to risk assessment
	// (e.g., memory usage, CPU bursts, temporary file creation).
	DeclaredSideEffects []string `json:"declared_side_effects,omitempty"`
	// EstimatedDurationSeconds is a best-effort upper bound on how long the
	// run phase will take. Zero means unestimated.
	EstimatedDurationSeconds int `json:"estimated_duration_seconds,omitempty"`
	// GeneratedAt is the timestamp when the plan was produced.
	GeneratedAt string `json:"generated_at"`
}

// NewPolicyPlan builds a PolicyPlan for the given adapter and request. Callers
// provide the plan contents directly; use this constructor to ensure the schema
// version and timestamp are always set correctly.
func NewPolicyPlan(adapter, jobID string, externalConsequences, requiredApprovals, workspaceBoundaries, sideEffects []string, estimatedDurationSeconds int, now time.Time) (PolicyPlan, error) {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		return PolicyPlan{}, fmt.Errorf("policy plan adapter is required")
	}
	return PolicyPlan{
		SchemaVersion:            PolicyPlanSchemaVersion,
		Adapter:                  adapter,
		JobID:                    strings.TrimSpace(jobID),
		ExternalConsequences:     cloneStringSlice(externalConsequences),
		RequiredApprovals:        cloneStringSlice(requiredApprovals),
		WorkspaceBoundaries:      cloneStringSlice(workspaceBoundaries),
		DeclaredSideEffects:      cloneStringSlice(sideEffects),
		EstimatedDurationSeconds: estimatedDurationSeconds,
		GeneratedAt:              now.UTC().Format(time.RFC3339Nano),
	}, nil
}

// PolicyPlanContract defines the checks to apply when verifying a PolicyPlan.
type PolicyPlanContract struct {
	Adapter                     string
	RequireExternalConsequences bool // plan must declare at least one external consequence
	RequireApprovals            bool // plan must list at least one required approval
	RequireWorkspaceBoundaries  bool // plan must list at least one boundary
}

// PolicyPlanReport is the result of verifying a PolicyPlan against a contract.
type PolicyPlanReport struct {
	SchemaVersion string  `json:"schema_version"`
	Adapter       string  `json:"adapter"`
	OK            bool    `json:"ok"`
	Checks        []Check `json:"checks"`
}

// VerifyPolicyPlanJSON verifies a serialised PolicyPlan against the supplied
// contract. It is intended to be called from adapter lifecycle tests and from
// the host kernel before approving the prepare phase.
func VerifyPolicyPlanJSON(content []byte, contract PolicyPlanContract) PolicyPlanReport {
	report := PolicyPlanReport{
		SchemaVersion: ConformanceReportSchemaVersion,
		Adapter:       contract.Adapter,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	var plan map[string]any
	if err := json.Unmarshal(content, &plan); err != nil {
		add("json_valid", false, err.Error())
		report.OK = allChecksPassed(report.Checks)
		return report
	}
	add("json_valid", true, "")
	add("schema_version", stringField(plan, "schema_version") == PolicyPlanSchemaVersion, stringField(plan, "schema_version"))
	add("adapter", strings.TrimSpace(contract.Adapter) == "" || stringField(plan, "adapter") == contract.Adapter, stringField(plan, "adapter"))
	add("generated_at_valid", validRFC3339(stringField(plan, "generated_at")), stringField(plan, "generated_at"))

	consequences := stringArrayField(plan, "external_consequences")
	add("external_consequences_declared", len(consequences) > 0 || !contract.RequireExternalConsequences,
		fmt.Sprintf("%d declared", len(consequences)))

	approvals := stringArrayField(plan, "required_approvals")
	add("required_approvals_declared", len(approvals) > 0 || !contract.RequireApprovals,
		fmt.Sprintf("%d declared", len(approvals)))

	boundaries := stringArrayField(plan, "workspace_boundaries")
	add("workspace_boundaries_declared", len(boundaries) > 0 || !contract.RequireWorkspaceBoundaries,
		fmt.Sprintf("%d declared", len(boundaries)))

	report.OK = allChecksPassed(report.Checks)
	return report
}

func allChecksPassed(checks []Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return true
}

func cloneStringSlice(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, v := range s {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
