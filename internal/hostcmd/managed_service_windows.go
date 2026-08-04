//go:build windows

package hostcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func (a App) service(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("service action is required: install, update, run, or uninstall")
	}
	switch args[0] {
	case "install":
		return a.installManagedService(args[1:])
	case "update":
		return a.updateManagedService(args[1:])
	case "run":
		return a.runManagedService(ctx, args[1:])
	case "uninstall":
		return a.uninstallManagedService(args[1:])
	default:
		return fmt.Errorf("unsupported service action %q", args[0])
	}
}

func (a App) installManagedService(args []string) error {
	fs := flag.NewFlagSet("rdev-host service install", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	serviceName := fs.String("service-name", defaultManagedServiceName, "Windows service name")
	replaceExisting := fs.Bool("replace-existing", false, "replace an installed managed service while preserving its protected state")
	gatewayURL := fs.String("gateway", "", "Control Plane session gateway URL")
	joinCode := fs.String("join-code", "", "Control Plane session join code")
	stateRoot := fs.String("state-root", "", "protected service state directory")
	identitySource := fs.String("identity-source", "", "optional existing host identity file to migrate")
	trustSource := fs.String("trust-source", "", "optional existing trust store file to migrate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected service install arguments: %s", strings.Join(fs.Args(), " "))
	}
	root, err := validateManagedServiceStateRoot(*stateRoot)
	if err != nil {
		return err
	}
	config := managedServiceConfig{
		ServiceName: strings.TrimSpace(*serviceName),
		GatewayURL:  strings.TrimSpace(*gatewayURL),
		JoinCode:    strings.TrimSpace(*joinCode),
		StateRoot:   root,
	}
	if err := config.validateWithSchema(); err != nil {
		return err
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager (run from the visible UAC-approved process): %w", err)
	}
	defer manager.Disconnect()

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate host executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve host executable: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create managed service state root: %w", err)
	}
	if err := protectManagedServiceState(root); err != nil {
		return err
	}
	binaryPath, err := prepareManagedServiceRelease(root, executable)
	if err != nil {
		return err
	}
	configPath := filepath.Join(root, managedServiceConfigFilename)
	if existing, err := manager.OpenService(config.ServiceName); err == nil {
		defer existing.Close()
		if !*replaceExisting {
			return fmt.Errorf("managed service %q is already installed", config.ServiceName)
		}
		return a.replaceManagedService(existing, binaryPath, configPath, config)
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("inspect managed service %q: %w", config.ServiceName, err)
	}
	if err := copyManagedServiceFileIfPresent(*identitySource, filepath.Join(root, managedServiceIdentityFile)); err != nil {
		return fmt.Errorf("migrate host identity: %w", err)
	}
	if err := copyManagedServiceFileIfPresent(*trustSource, filepath.Join(root, managedServiceTrustFile)); err != nil {
		return fmt.Errorf("migrate trust store: %w", err)
	}
	if err := writeManagedServiceConfig(configPath, config); err != nil {
		return err
	}

	service, err := manager.CreateService(config.ServiceName, binaryPath, mgr.Config{
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
		DisplayName:  "Remote Dev Skillkit Host",
		Description:  "Outbound managed Remote Dev Skillkit connector",
	}, "service", "run", "--config", configPath)
	if err != nil {
		return fmt.Errorf("install managed Windows service: %w", err)
	}
	defer service.Close()
	if err := service.Start(); err != nil {
		service.Delete()
		return fmt.Errorf("start managed Windows service: %w", err)
	}
	_, err = fmt.Fprintf(a.Stdout, "managed service installed and started: %s\n", config.ServiceName)
	return err
}

func (a App) replaceManagedService(service *mgr.Service, binaryPath, configPath string, replacement managedServiceConfig) error {
	previousConfig, err := readManagedServiceConfig(configPath)
	if err != nil {
		return fmt.Errorf("read installed managed service config before replace: %w", err)
	}
	previousServiceConfig, err := service.Config()
	if err != nil {
		return fmt.Errorf("read installed managed service settings before replace: %w", err)
	}
	if err := stopManagedService(service); err != nil {
		return err
	}
	if err := writeManagedServiceConfig(configPath, replacement); err != nil {
		return rollbackManagedServiceReplacement(service, previousServiceConfig, configPath, previousConfig, fmt.Errorf("write replacement managed service config: %w", err))
	}
	nextServiceConfig := previousServiceConfig
	nextServiceConfig.BinaryPathName = managedServiceCommandLine(binaryPath, configPath)
	nextServiceConfig.StartType = mgr.StartAutomatic
	nextServiceConfig.ErrorControl = mgr.ErrorNormal
	nextServiceConfig.DisplayName = "Remote Dev Skillkit Host"
	nextServiceConfig.Description = "Outbound managed Remote Dev Skillkit connector"
	if err := service.UpdateConfig(nextServiceConfig); err != nil {
		return rollbackManagedServiceReplacement(service, previousServiceConfig, configPath, previousConfig, fmt.Errorf("update managed Windows service: %w", err))
	}
	if err := service.Start(); err != nil {
		return rollbackManagedServiceReplacement(service, previousServiceConfig, configPath, previousConfig, fmt.Errorf("start replacement managed Windows service: %w", err))
	}
	_, err = fmt.Fprintf(a.Stdout, "managed service replaced and started: %s\n", replacement.ServiceName)
	return err
}

func rollbackManagedServiceReplacement(service *mgr.Service, previousServiceConfig mgr.Config, configPath string, previousConfig managedServiceConfig, cause error) error {
	if err := stopManagedService(service); err != nil {
		return fmt.Errorf("%w; stop replacement before rollback: %v", cause, err)
	}
	if err := writeManagedServiceConfig(configPath, previousConfig); err != nil {
		return fmt.Errorf("%w; restore managed service config: %v", cause, err)
	}
	if err := service.UpdateConfig(previousServiceConfig); err != nil {
		return fmt.Errorf("%w; restore managed Windows service settings: %v", cause, err)
	}
	if err := service.Start(); err != nil {
		return fmt.Errorf("%w; restart previous managed Windows service: %v", cause, err)
	}
	return cause
}

func managedServiceCommandLine(binaryPath, configPath string) string {
	return strings.Join([]string{syscall.EscapeArg(binaryPath), "service", "run", "--config", syscall.EscapeArg(configPath)}, " ")
}

func (a App) updateManagedService(args []string) error {
	fs := flag.NewFlagSet("rdev-host service update", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	serviceName := fs.String("service-name", "", "Windows service name (default: discover by current executable)")
	releaseDir := fs.String("release", "", "staged release directory containing rdev-host.exe and rdev-host.exe.sha256")
	healthWaitSeconds := fs.Int("health-wait-seconds", 60, "bounded SCM health window before declaring the replacement healthy")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected service update arguments: %s", strings.Join(fs.Args(), " "))
	}
	release := filepath.Clean(strings.TrimSpace(*releaseDir))
	if release == "" || release == "." {
		return fmt.Errorf("release directory is required")
	}
	stagedBinary := filepath.Join(release, managedServiceBinaryFilename)
	if err := verifyStagedManagedServiceRelease(release, stagedBinary); err != nil {
		return err
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()

	name := strings.TrimSpace(*serviceName)
	if name == "" {
		name, err = discoverManagedServiceByExecutable(manager)
		if err != nil {
			return err
		}
	}
	service, err := manager.OpenService(name)
	if err != nil {
		return fmt.Errorf("open managed service %q: %w", name, err)
	}
	defer service.Close()

	previousSCMConfig, err := service.Config()
	if err != nil {
		return fmt.Errorf("read installed managed service settings: %w", err)
	}
	configPath, err := managedServiceConfigPathFromCommandLine(previousSCMConfig.BinaryPathName)
	if err != nil {
		return err
	}
	previousConfig, err := readManagedServiceConfig(configPath)
	if err != nil {
		return err
	}
	binaryPath, err := prepareManagedServiceRelease(previousConfig.StateRoot, stagedBinary)
	if err != nil {
		return fmt.Errorf("stage managed service release: %w", err)
	}
	if err := a.replaceManagedService(service, binaryPath, configPath, previousConfig); err != nil {
		return err
	}
	if err := waitManagedServiceHealthy(service, binaryPath, *healthWaitSeconds); err != nil {
		rollbackErr := rollbackManagedServiceReplacement(service, previousSCMConfig, configPath, previousConfig, err)
		_ = writeManagedServiceUpdateResult(release, false, err, rollbackErr)
		return rollbackErr
	}
	if err := writeManagedServiceUpdateResult(release, true, nil, nil); err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.Stdout, "managed service updated and running: %s (%s)\n", name, binaryPath)
	return err
}

// verifyStagedManagedServiceRelease confirms the staged binary matches the
// digest the updater recorded when it downloaded the artifact, so a corrupted
// or raced staging directory can never be activated.
func verifyStagedManagedServiceRelease(releaseDir, stagedBinary string) error {
	expected, err := os.ReadFile(filepath.Join(releaseDir, managedServiceBinaryFilename+".sha256"))
	if err != nil {
		return fmt.Errorf("read staged release digest: %w", err)
	}
	actual, err := managedServiceFileSHA256(stagedBinary)
	if err != nil {
		return fmt.Errorf("hash staged release binary: %w", err)
	}
	if actual != strings.ToLower(strings.TrimSpace(string(expected))) {
		return fmt.Errorf("staged release digest mismatch: got %s want %s", actual, strings.TrimSpace(string(expected)))
	}
	return nil
}

// discoverManagedServiceByExecutable finds the SCM service whose binary path
// matches the current executable, so an unattended updater does not need to
// know the service name or its config path in advance.
func discoverManagedServiceByExecutable(manager *mgr.Mgr) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate host executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve host executable: %w", err)
	}
	names, err := manager.ListServices()
	if err != nil {
		return "", fmt.Errorf("list Windows services: %w", err)
	}
	for _, name := range names {
		service, err := manager.OpenService(name)
		if err != nil {
			continue
		}
		cfg, err := service.Config()
		_ = service.Close()
		if err != nil {
			continue
		}
		binary := managedServiceExecutablePath(cfg.BinaryPathName)
		if binary != "" && strings.EqualFold(filepath.Base(binary), filepath.Base(executable)) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no managed service running this executable was found")
}

func managedServiceExecutablePath(commandLine string) string {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return ""
	}
	if commandLine[0] == '"' {
		if end := strings.Index(commandLine[1:], `"`); end >= 0 {
			return commandLine[1 : 1+end]
		}
		return ""
	}
	if end := strings.IndexAny(commandLine, " \t"); end >= 0 {
		return commandLine[:end]
	}
	return commandLine
}

func managedServiceConfigPathFromCommandLine(commandLine string) (string, error) {
	fields := splitManagedServiceCommandLine(commandLine)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "--config" {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("managed service command line does not declare --config")
}

func splitManagedServiceCommandLine(commandLine string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false
	for _, r := range commandLine {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case (r == ' ' || r == '\t') && !inQuotes:
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

// waitManagedServiceHealthy waits until SCM reports the replacement binary
// actually running. A replacement that stops during the window is a boot
// failure of the new build; the caller rolls back to the previous release.
func waitManagedServiceHealthy(service *mgr.Service, expectedBinaryPath string, seconds int) error {
	if seconds <= 0 {
		seconds = 60
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		cfg, err := service.Config()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		binary := managedServiceExecutablePath(cfg.BinaryPathName)
		if status.State == svc.Running && strings.EqualFold(filepath.Clean(binary), filepath.Clean(expectedBinaryPath)) {
			return nil
		}
		if status.State == svc.Stopped {
			return fmt.Errorf("replacement managed service stopped during the health window")
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("replacement managed service did not run %s within %ds", expectedBinaryPath, seconds)
}

// writeManagedServiceUpdateResult records the updater outcome next to the
// staged release so the operator can read it back after the service
// reconnects (the updater process is detached and its exit code is not
// otherwise observable).
func writeManagedServiceUpdateResult(releaseDir string, ok bool, updateErr, rollbackErr error) error {
	result := map[string]any{
		"schema_version": "rdev.host-update-result.v1",
		"ok":             ok,
		"at":             time.Now().UTC().Format(time.RFC3339),
	}
	if updateErr != nil {
		result["error"] = updateErr.Error()
	}
	if rollbackErr != nil {
		result["rollback_error"] = rollbackErr.Error()
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(releaseDir, "UPDATE_RESULT.json"), content, 0o600)
}

func (a App) runManagedService(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rdev-host service run", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	configPath := fs.String("config", "", "managed service config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected service run arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*configPath) == "" {
		return fmt.Errorf("managed service config is required")
	}
	config, err := readManagedServiceConfig(*configPath)
	if err != nil {
		return err
	}
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect Windows service context: %w", err)
	}
	if !isService {
		return fmt.Errorf("managed service run must be launched by the Windows Service Control Manager")
	}
	return svc.Run(config.ServiceName, managedServiceHandler{app: a, parent: ctx, config: config})
}

func (a App) uninstallManagedService(args []string) error {
	fs := flag.NewFlagSet("rdev-host service uninstall", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	serviceName := fs.String("service-name", defaultManagedServiceName, "Windows service name")
	stateRoot := fs.String("state-root", "", "protected service state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected service uninstall arguments: %s", strings.Join(fs.Args(), " "))
	}
	root, err := validateManagedServiceStateRoot(*stateRoot)
	if err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(strings.TrimSpace(*serviceName))
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return os.RemoveAll(root)
		}
		return fmt.Errorf("open managed service: %w", err)
	}
	if err := stopManagedService(service); err != nil {
		service.Close()
		return err
	}
	if err := service.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		service.Close()
		return fmt.Errorf("remove managed service: %w", err)
	}
	if err := service.Close(); err != nil {
		return fmt.Errorf("close managed service handle: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove managed service state: %w", err)
	}
	_, err = fmt.Fprintf(a.Stdout, "managed service removed: %s\n", *serviceName)
	return err
}

type managedServiceHandler struct {
	app    App
	parent context.Context
	config managedServiceConfig
}

func (h managedServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending, WaitHint: 10_000}
	ctx, cancel := context.WithCancel(h.parent)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runManagedServiceWithRetry(ctx, managedServiceRetryDelay, func(ctx context.Context) error {
			return h.app.runServe(ctx, h.config.serveOptions())
		})
	}()
	status := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	changes <- status
	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case request, ok := <-requests:
			if !ok {
				cancel()
				<-done
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				changes <- status
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: 10_000}
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		}
	}
}

func stopManagedService(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query managed service: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop managed service: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, err = service.Query()
		if err == nil && status.State == svc.Stopped {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out stopping managed service")
}

func validateManagedServiceStateRoot(path string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(path))
	if root == "." || root == "" || !filepath.IsAbs(root) || filepath.Dir(root) == root {
		return "", fmt.Errorf("managed service state root must be a non-root absolute path")
	}
	return root, nil
}

func (c managedServiceConfig) validateWithSchema() error {
	c.SchemaVersion = managedServiceConfigSchema
	return c.validate()
}

func protectManagedServiceState(root string) error {
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("create administrators SID: %w", err)
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("create LocalSystem SID: %w", err)
	}
	access := []windows.EXPLICIT_ACCESS{
		{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_GROUP, TrusteeValue: windows.TrusteeValueFromSID(adminSID)}},
		{AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(systemSID)}},
	}
	acl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return fmt.Errorf("build managed service ACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("protect managed service state: %w", err)
	}
	return nil
}
