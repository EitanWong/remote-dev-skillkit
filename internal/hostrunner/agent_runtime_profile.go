package hostrunner

import (
	"fmt"
	"os"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/toolchain"
)

type agentRuntimeProfile struct {
	Command      string
	CodexProfile string
	Environment  map[string]string
}

func resolveAgentRuntimeProfile(envelope taskEnvelope, adapter string, options Options) (agentRuntimeProfile, error) {
	profileID := strings.TrimSpace(stringValue(envelope.Payload, "toolchain_profile_id", ""))
	if profileID == "" {
		return agentRuntimeProfile{}, nil
	}
	profile, err := toolchain.LoadRuntimeProfileByID(options.ToolchainRoot, profileID)
	if err != nil {
		return agentRuntimeProfile{}, err
	}
	if profile.Tool != adapter {
		return agentRuntimeProfile{}, fmt.Errorf("toolchain profile %q does not match adapter %q", profileID, adapter)
	}
	environment, err := profile.LaunchEnvironment(os.LookupEnv)
	if err != nil {
		return agentRuntimeProfile{}, err
	}
	return agentRuntimeProfile{
		Command:      profile.Command,
		CodexProfile: profile.CodexProfile,
		Environment:  environment,
	}, nil
}

func configuredAgentCommand(envelope taskEnvelope, fields ...string) string {
	for _, field := range fields {
		if command := strings.TrimSpace(stringValue(envelope.Payload, field, "")); command != "" {
			return command
		}
	}
	return ""
}

func hasToolchainProfile(envelope taskEnvelope) bool {
	return strings.TrimSpace(stringValue(envelope.Payload, "toolchain_profile_id", "")) != ""
}
