//go:build windows

package windowsentry

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

const (
	winCreateSuspended               = 0x00000004
	winJobExtendedLimitInfo          = 9
	winJobBasicAccountingInfo        = 1
	winJobLimitKillOnClose           = 0x00002000
	winProcessTerminate              = 0x0001
	winProcessSetQuota               = 0x0100
	winThreadSuspendResume           = 0x0002
	winInvalidSuspendCount    uint32 = 0xffffffff
)

var (
	winCreateJobObjectW          = winKernel32.NewProc("CreateJobObjectW")
	winSetInformationJobObject   = winKernel32.NewProc("SetInformationJobObject")
	winAssignProcessToJobObject  = winKernel32.NewProc("AssignProcessToJobObject")
	winTerminateJobObject        = winKernel32.NewProc("TerminateJobObject")
	winQueryInformationJobObject = winKernel32.NewProc("QueryInformationJobObject")
	winThread32First             = winKernel32.NewProc("Thread32First")
	winThread32Next              = winKernel32.NewProc("Thread32Next")
	winOpenThread                = winKernel32.NewProc("OpenThread")
	winResumeThread              = winKernel32.NewProc("ResumeThread")
	errWindowsCoreLifecycle      = errors.New("core lifecycle failure")
)

type winThreadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}

type coreLifecycle struct {
	job syscall.Handle
}

func newCoreLifecycle(cmd *exec.Cmd) (*coreLifecycle, error) {
	if cmd == nil {
		return nil, errWindowsCoreLifecycle
	}
	jobValue, _, callErr := winCreateJobObjectW.Call(0, 0)
	if jobValue == 0 {
		return nil, winCallError(callErr)
	}
	lifecycle := &coreLifecycle{job: syscall.Handle(jobValue)}
	limits := [18]uint64{}
	*(*uint32)(unsafe.Pointer(&limits[2])) = winJobLimitKillOnClose
	result, _, callErr := winSetInformationJobObject.Call(
		jobValue,
		winJobExtendedLimitInfo,
		uintptr(unsafe.Pointer(&limits)),
		unsafe.Sizeof(limits),
	)
	if result == 0 {
		_ = callErr
		_ = lifecycle.close()
		return nil, errWindowsCoreLifecycle
	}
	attributes := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attributes = *cmd.SysProcAttr
	}
	attributes.CreationFlags |= winCreateSuspended
	cmd.SysProcAttr = &attributes
	return lifecycle, nil
}

func (lifecycle *coreLifecycle) run(_ context.Context, cmd *exec.Cmd) (bool, error) {
	if err := cmd.Start(); err != nil {
		_ = lifecycle.close()
		return true, errWindowsCoreLifecycle
	}
	assigned, err := lifecycle.assign(cmd.Process.Pid)
	if err != nil {
		return lifecycle.abort(cmd, assigned)
	}
	if err := resumeWindowsProcess(cmd.Process.Pid); err != nil {
		return lifecycle.abort(cmd, true)
	}
	waitErr := cmd.Wait()
	terminateErr := lifecycle.terminate()
	emptyErr := lifecycle.waitEmpty()
	closeErr := lifecycle.close()
	if waitErr != nil || terminateErr != nil || emptyErr != nil || closeErr != nil {
		return emptyErr == nil, errWindowsCoreLifecycle
	}
	return true, nil
}

func (lifecycle *coreLifecycle) assign(processID int) (bool, error) {
	process, err := syscall.OpenProcess(winProcessTerminate|winProcessSetQuota, false, uint32(processID))
	if err != nil {
		return false, errWindowsCoreLifecycle
	}
	result, _, callErr := winAssignProcessToJobObject.Call(uintptr(lifecycle.job), uintptr(process))
	closeErr := syscall.CloseHandle(process)
	if result == 0 {
		_ = callErr
		return false, errWindowsCoreLifecycle
	}
	if closeErr != nil {
		return true, errWindowsCoreLifecycle
	}
	return true, nil
}

func (lifecycle *coreLifecycle) abort(cmd *exec.Cmd, assigned bool) (bool, error) {
	if assigned {
		terminateErr := lifecycle.terminate()
		if terminateErr != nil {
			closeErr := lifecycle.close()
			if closeErr == nil {
				_ = cmd.Wait()
				return false, errWindowsCoreLifecycle
			}
			killErr := cmd.Process.Kill()
			if killErr == nil || errors.Is(killErr, os.ErrProcessDone) {
				_ = cmd.Wait()
			}
			return false, errWindowsCoreLifecycle
		}
		_ = cmd.Wait()
		emptyErr := lifecycle.waitEmpty()
		_ = lifecycle.close()
		return emptyErr == nil, errWindowsCoreLifecycle
	}
	killErr := cmd.Process.Kill()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		_ = lifecycle.close()
		return false, errWindowsCoreLifecycle
	}
	_ = cmd.Wait()
	_ = lifecycle.close()
	return true, errWindowsCoreLifecycle
}

func (lifecycle *coreLifecycle) terminate() error {
	if lifecycle == nil || lifecycle.job == 0 {
		return errWindowsCoreLifecycle
	}
	result, _, callErr := winTerminateJobObject.Call(uintptr(lifecycle.job), 1)
	if result == 0 {
		return winCallError(callErr)
	}
	return nil
}

func (lifecycle *coreLifecycle) waitEmpty() error {
	if lifecycle == nil || lifecycle.job == 0 {
		return errWindowsCoreLifecycle
	}
	for attempt := 0; attempt < 1000; attempt++ {
		information := [6]uint64{}
		result, _, callErr := winQueryInformationJobObject.Call(
			uintptr(lifecycle.job),
			winJobBasicAccountingInfo,
			uintptr(unsafe.Pointer(&information)),
			unsafe.Sizeof(information),
			0,
		)
		if result == 0 {
			return winCallError(callErr)
		}
		if *(*uint32)(unsafe.Pointer(&information[5])) == 0 {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errWindowsCoreLifecycle
}

func (lifecycle *coreLifecycle) close() error {
	if lifecycle == nil || lifecycle.job == 0 {
		return nil
	}
	err := syscall.CloseHandle(lifecycle.job)
	if err == nil {
		lifecycle.job = 0
	}
	return err
}

func resumeWindowsProcess(processID int) (resultErr error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() {
		if syscall.CloseHandle(snapshot) != nil {
			resultErr = errWindowsCoreLifecycle
		}
	}()
	entry := winThreadEntry32{Size: uint32(unsafe.Sizeof(winThreadEntry32{}))}
	result, _, callErr := winThread32First.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	for result != 0 {
		if entry.OwnerProcessID == uint32(processID) {
			threadValue, _, openErr := winOpenThread.Call(winThreadSuspendResume, 0, uintptr(entry.ThreadID))
			if threadValue == 0 {
				_ = openErr
				return errWindowsCoreLifecycle
			}
			resumed, _, resumeErr := winResumeThread.Call(threadValue)
			closeErr := syscall.CloseHandle(syscall.Handle(threadValue))
			if uint32(resumed) == winInvalidSuspendCount {
				_ = resumeErr
				return errWindowsCoreLifecycle
			}
			if closeErr != nil {
				return errWindowsCoreLifecycle
			}
			return nil
		}
		result, _, callErr = winThread32Next.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	}
	if callErr != syscall.ERROR_NO_MORE_FILES {
		return errWindowsCoreLifecycle
	}
	return errWindowsCoreLifecycle
}
