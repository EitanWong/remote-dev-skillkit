package acceptance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManagedMacAcceptanceDoesNotUseRetiredJobProtocol(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	sourcePath := filepath.Join(filepath.Dir(file), "managed_mac.go")
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{
		"CodingJob",
		"AuthorizationJob",
		"model.Job ",
		"model.Job{",
		"model.JobStatus",
		"model.Artifact",
		"CreateJob",
		"CompleteJob",
		"FailJob",
		"RunDevJob",
		"hostnonce",
		"hostauthorization",
		"evidence.Input",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("managed Mac acceptance must use session/task protocol, found retired symbol %q", forbidden)
		}
	}
}
