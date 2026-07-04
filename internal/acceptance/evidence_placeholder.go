package acceptance

import (
	"encoding/json"
	"strings"
)

func evidenceContentIsPlaceholder(content []byte) bool {
	lower := strings.ToLower(string(content))
	if strings.Contains(lower, "placeholder only - replace with real redacted evidence before packaging") ||
		strings.Contains(lower, `"replace_before_packaging": true`) ||
		strings.Contains(lower, `"replace_before_packaging":true`) {
		return true
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return false
	}
	return jsonHasBoolField(value, "placeholder", true) && jsonHasBoolField(value, "replace_before_packaging", true)
}

func acceptanceEvidencePath(path string) bool {
	return strings.HasPrefix(path, "evidence/")
}
