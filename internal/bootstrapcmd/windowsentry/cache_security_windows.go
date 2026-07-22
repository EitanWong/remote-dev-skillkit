//go:build windows

package windowsentry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type windowsPrivateTrustees struct {
	current        string
	administrators string
	system         string
}

func preparePrivateCache(root string, directories []string, files []managedCacheFile) error {
	if err := validateWindowsCacheRoot(root); err != nil {
		return err
	}
	return prepareWindowsPrivateState(directories, files)
}

func prepareWindowsPrivateState(directories []string, files []managedCacheFile) error {
	descriptor, trustees, err := windowsPrivateSecurityDescriptor()
	if err != nil {
		return err
	}
	defer descriptor.close()
	for _, directory := range directories {
		if err := ensureWindowsPrivateDirectory(directory, descriptor, trustees); err != nil {
			return err
		}
	}
	for _, file := range files {
		if err := ensureWindowsPrivateManagedFile(file, trustees); err != nil {
			return err
		}
	}
	return nil
}

func ensureWindowsPrivateManagedFile(file managedCacheFile, trustees windowsPrivateTrustees) error {
	_, err := windowsPathAttributes(file.path)
	if windowsPathMissing(err) {
		created, createErr := createPrivateWindowsFile(filepath.Dir(file.path), filepath.Base(file.path), syscall.FILE_ATTRIBUTE_NORMAL)
		if createErr != nil && !errors.Is(createErr, syscall.ERROR_ALREADY_EXISTS) && !errors.Is(createErr, syscall.ERROR_FILE_EXISTS) {
			return createErr
		}
		if createErr == nil {
			if closeErr := created.Close(); closeErr != nil {
				return closeErr
			}
		}
		return validateExistingWindowsManagedFile(file, trustees)
	}
	if err != nil {
		return err
	}
	return validateExistingWindowsManagedFile(file, trustees)
}

func validateWindowsCacheRoot(root string) error {
	if err := validateWindowsLocalPath(root); err != nil {
		return err
	}
	localAppData, err := winKnownLocalAppData()
	if err != nil {
		return fmt.Errorf("resolve Windows LocalAppData: %w", err)
	}
	base := filepath.Join(filepath.Clean(localAppData), "RemoteDevSkillkit", "cache")
	basePrefix := base + string(os.PathSeparator)
	if !strings.EqualFold(root, base) && (len(root) <= len(basePrefix) || !strings.EqualFold(root[:len(basePrefix)], basePrefix)) {
		return fmt.Errorf("layered cache must remain under LocalAppData\\RemoteDevSkillkit\\cache")
	}
	return rejectWindowsReparseAncestors(root)
}

func validateWindowsLocalPath(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if path != clean || !filepath.IsAbs(clean) || strings.HasPrefix(clean, `\\`) || len(volume) != 2 || volume[1] != ':' {
		return fmt.Errorf("layered path must use an absolute local Windows drive")
	}
	rootPointer, err := syscall.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return err
	}
	driveType := winGetDriveType(rootPointer)
	if driveType < winDriveRemovable || driveType > winDriveRAMDisk || driveType == winDriveRemote {
		return fmt.Errorf("layered path must use a local Windows volume")
	}
	return nil
}

func rejectWindowsReparseAncestors(path string) error {
	if err := validateWindowsLocalPath(path); err != nil {
		return err
	}
	volumeRoot := filepath.VolumeName(path) + `\`
	relative, err := filepath.Rel(volumeRoot, path)
	if err != nil {
		return err
	}
	current := volumeRoot
	for _, component := range strings.Split(relative, `\`) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		attributes, err := windowsPathAttributes(current)
		if windowsPathMissing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("layered path must not traverse a Windows reparse point")
		}
	}
	return nil
}

func ensureWindowsPrivateDirectory(path string, descriptor winSecurityDescriptor, trustees windowsPrivateTrustees) error {
	attributes, err := windowsPathAttributes(path)
	if err == nil {
		if attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("layered cache path is not a private directory")
		}
		return validateWindowsPrivatePath(path, true, true, trustees)
	}
	if !windowsPathMissing(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return fmt.Errorf("create layered cache directory without a parent")
	}
	if err := ensureWindowsPrivateParent(parent, descriptor, trustees); err != nil {
		return err
	}
	if err := createWindowsPrivateDirectory(path, descriptor); err != nil && !errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
		return err
	}
	return validateWindowsPrivatePath(path, true, true, trustees)
}

func ensureWindowsPrivateParent(path string, descriptor winSecurityDescriptor, trustees windowsPrivateTrustees) error {
	attributes, err := windowsPathAttributes(path)
	if err == nil {
		if attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("layered cache parent is not a local directory")
		}
		return nil
	}
	if !windowsPathMissing(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return fmt.Errorf("layered cache parent does not exist")
	}
	if err := ensureWindowsPrivateParent(parent, descriptor, trustees); err != nil {
		return err
	}
	if err := createWindowsPrivateDirectory(path, descriptor); err != nil && !errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
		return err
	}
	return validateWindowsPrivatePath(path, true, true, trustees)
}

func createWindowsPrivateDirectory(path string, descriptor winSecurityDescriptor) error {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return syscall.CreateDirectory(pointer, descriptor.attributes())
}

func windowsPrivateSecurityDescriptor() (winSecurityDescriptor, windowsPrivateTrustees, error) {
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return winSecurityDescriptor{}, windowsPrivateTrustees{}, err
	}
	descriptor, err := newWinSecurityDescriptor(fmt.Sprintf(
		"O:%sD:P(A;OICI;FA;;;%s)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)",
		trustees.current,
		trustees.current,
	))
	if err != nil {
		return winSecurityDescriptor{}, windowsPrivateTrustees{}, fmt.Errorf("create private Windows security descriptor: %w", err)
	}
	return descriptor, trustees, nil
}

func currentWindowsPrivateTrustees() (windowsPrivateTrustees, error) {
	current, err := currentWinUserSID()
	if err != nil {
		return windowsPrivateTrustees{}, err
	}
	return windowsPrivateTrustees{
		current:        current,
		administrators: "S-1-5-32-544",
		system:         "S-1-5-18",
	}, nil
}

func validateExistingWindowsManagedFile(file managedCacheFile, trustees windowsPrivateTrustees) error {
	attributes, err := windowsPathAttributes(file.path)
	if windowsPathMissing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("layered cache contains an unsafe managed file")
	}
	handle, err := openWindowsPrivateFile(file.path, false, trustees)
	if err != nil {
		return err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > file.maxBytes {
		return fmt.Errorf("layered cache contains an oversized or non-regular managed file")
	}
	return nil
}

func validatePrivateCacheFile(path string, expectedSize int64) error {
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return err
	}
	file, err := openWindowsPrivateFile(path, false, trustees)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expectedSize {
		return fmt.Errorf("layered cache file is not a private regular file with the signed size")
	}
	return nil
}

func openPrivateRuntime(path string) (*os.File, error) {
	trustees, err := currentWindowsPrivateTrustees()
	if err != nil {
		return nil, err
	}
	return openWindowsPrivateFile(path, true, trustees)
}

func openWindowsPrivateFile(path string, lock bool, trustees windowsPrivateTrustees) (*os.File, error) {
	if err := rejectWindowsReparseAncestors(filepath.Dir(path)); err != nil {
		return nil, err
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	share := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	if lock {
		share = syscall.FILE_SHARE_READ
	}
	handle, err := syscall.CreateFile(
		pointer,
		syscall.GENERIC_READ|winReadControl,
		share,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	if err := validateWindowsPrivateHandle(handle, false, true, 3, trustees); err != nil {
		syscall.CloseHandle(handle)
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("wrap private Windows file handle")
	}
	return file, nil
}

func validateWindowsPrivatePath(path string, directory, requireProtected bool, trustees windowsPrivateTrustees) error {
	if err := rejectWindowsReparseAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags := uint32(syscall.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= syscall.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := syscall.CreateFile(
		pointer,
		winReadControl|winFileReadAttributes,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	return validateWindowsPrivateHandle(handle, directory, requireProtected, 3, trustees)
}

func validateWindowsPrivateHandle(handle syscall.Handle, directory, requireProtected bool, expectedFlags int, trustees windowsPrivateTrustees) error {
	if err := validateWindowsPathType(handle, directory); err != nil {
		return err
	}
	state, err := readWinSecurityState(handle)
	if err != nil {
		return err
	}
	return validateWindowsPrivateSecurityState(state, requireProtected, expectedFlags, trustees)
}

func validateWindowsPrivateSecurityState(state winSecurityState, requireProtected bool, expectedFlags int, trustees windowsPrivateTrustees) error {
	if state.control&winSEDACLPresent == 0 || requireProtected && state.control&winSEDACLProtected == 0 {
		return fmt.Errorf("private Windows path must have a present protected DACL")
	}
	if !trustees.contains(state.owner) {
		return fmt.Errorf("private Windows path owner is not trusted")
	}
	if len(state.aces) != 3 {
		return fmt.Errorf("private Windows path DACL must contain exactly three ACEs")
	}
	seen := make(map[string]bool, 3)
	for _, ace := range state.aces {
		if ace.typeID != winAccessAllowedACEType || ace.mask != winPrivateFullControl || expectedFlags >= 0 && int(ace.flags) != expectedFlags {
			return fmt.Errorf("private Windows DACL contains an unsupported ACE")
		}
		if !trustees.contains(ace.sid) || seen[ace.sid] {
			return fmt.Errorf("private Windows DACL grants an untrusted SID")
		}
		seen[ace.sid] = true
	}
	if !seen[trustees.current] || !seen[trustees.administrators] || !seen[trustees.system] {
		return fmt.Errorf("private Windows DACL is missing a trusted trustee")
	}
	return nil
}

func validateWindowsPathType(handle syscall.Handle, directory bool) error {
	var information syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &information); err != nil {
		return err
	}
	isDirectory := information.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
	if information.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || isDirectory != directory {
		return fmt.Errorf("private Windows path has unsafe file attributes")
	}
	return nil
}

func (trustees windowsPrivateTrustees) contains(sid string) bool {
	return sid == trustees.current || sid == trustees.administrators || sid == trustees.system
}

func windowsPathAttributes(path string) (uint32, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.GetFileAttributes(pointer)
}

func windowsPathMissing(err error) bool {
	return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) || errors.Is(err, syscall.ERROR_PATH_NOT_FOUND)
}
