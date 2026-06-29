package shelladapter

import "regexp"

type redactionRule struct {
	name        string
	pattern     *regexp.Regexp
	replacement string
}

var defaultRedactionRules = []redactionRule{
	{
		name:        "private_key_block",
		pattern:     regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
		replacement: "[REDACTED:private_key_block]",
	},
	{
		name:        "authorization_bearer",
		pattern:     regexp.MustCompile(`(?i)\b(Authorization\s*:\s*Bearer\s+)([A-Za-z0-9._~+/=-]{8,})`),
		replacement: `${1}[REDACTED:authorization_bearer]`,
	},
	{
		name:        "openai_api_key",
		pattern:     regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
		replacement: "[REDACTED:openai_api_key]",
	},
	{
		name:        "github_pat",
		pattern:     regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
		replacement: "[REDACTED:github_pat]",
	},
	{
		name:        "github_token",
		pattern:     regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
		replacement: "[REDACTED:github_token]",
	},
	{
		name:        "aws_access_key_id",
		pattern:     regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`),
		replacement: "[REDACTED:aws_access_key_id]",
	},
	{
		name:        "secret_json",
		pattern:     regexp.MustCompile(`(?i)("(?:password|passwd|token|api[_-]?key|secret|access[_-]?token)"\s*:\s*")([^"]{4,})(")`),
		replacement: `${1}[REDACTED:secret_json]${3}`,
	},
	{
		name:        "secret_assignment",
		pattern:     regexp.MustCompile(`(?i)\b(password|passwd|token|api[_-]?key|secret|access[_-]?token)(\s*[:=]\s*)([^\s&;,"'\[]{4,})`),
		replacement: `${1}${2}[REDACTED:secret_assignment]`,
	},
}

type redactor struct {
	counts map[string]int
}

type ArtifactRedactor struct {
	redactor *redactor
}

func newRedactor() *redactor {
	return &redactor{counts: map[string]int{}}
}

func NewArtifactRedactor() *ArtifactRedactor {
	return &ArtifactRedactor{redactor: newRedactor()}
}

func RedactionRuleNames() []string {
	names := make([]string, 0, len(defaultRedactionRules))
	for _, rule := range defaultRedactionRules {
		names = append(names, rule.name)
	}
	return names
}

func (r *ArtifactRedactor) Redact(input string) string {
	if r == nil || r.redactor == nil {
		return input
	}
	return r.redactor.Redact(input)
}

func (r *ArtifactRedactor) Redacted() bool {
	if r == nil || r.redactor == nil {
		return false
	}
	return r.redactor.Redacted()
}

func (r *ArtifactRedactor) Counts() map[string]int {
	if r == nil || r.redactor == nil {
		return nil
	}
	return r.redactor.Counts()
}

func (r *redactor) Redact(input string) string {
	output := input
	for _, rule := range defaultRedactionRules {
		matches := rule.pattern.FindAllStringIndex(output, -1)
		if len(matches) == 0 {
			continue
		}
		r.counts[rule.name] += len(matches)
		output = rule.pattern.ReplaceAllString(output, rule.replacement)
	}
	return output
}

func (r *redactor) Redacted() bool {
	for _, count := range r.counts {
		if count > 0 {
			return true
		}
	}
	return false
}

func (r *redactor) Counts() map[string]int {
	if len(r.counts) == 0 {
		return nil
	}
	counts := make(map[string]int, len(r.counts))
	for name, count := range r.counts {
		counts[name] = count
	}
	return counts
}
