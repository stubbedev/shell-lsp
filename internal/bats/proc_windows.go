//go:build windows

package bats

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	createSuspended            = 0x00000004
	jobObjectExtendedLimitInfo = 9
	jobLimitKillOnJobClose     = 0x00002000
	processSetQuota            = 0x0100
	snapshotThreads            = 0x00000004
	threadSuspendResume        = 0x0002
	resumeFailed               = 0xFFFFFFFF
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW        = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob      = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject      = kernel32.NewProc("TerminateJobObject")
	procThread32First           = kernel32.NewProc("Thread32First")
	procThread32Next            = kernel32.NewProc("Thread32Next")
	procOpenThread              = kernel32.NewProc("OpenThread")
	procResumeThread            = kernel32.NewProc("ResumeThread")
)

type jobBasicLimits struct {
	perProcessUserTimeLimit int64
	perJobUserTimeLimit     int64
	limitFlags              uint32
	minimumWorkingSetSize   uintptr
	maximumWorkingSetSize   uintptr
	activeProcessLimit      uint32
	affinity                uintptr
	priorityClass           uint32
	schedulingClass         uint32
}

type ioCounters struct {
	readOperations, writeOperations, otherOperations uint64
	readBytes, writeBytes, otherBytes                uint64
}

type jobExtendedLimits struct {
	basic                 jobBasicLimits
	io                    ioCounters
	processMemoryLimit    uintptr
	jobMemoryLimit        uintptr
	peakProcessMemoryUsed uintptr
	peakJobMemoryUsed     uintptr
}

type threadEntry struct {
	size           uint32
	usage          uint32
	threadID       uint32
	ownerProcessID uint32
	basePriority   int32
	deltaPriority  int32
	flags          uint32
}

func startTree(cmd *exec.Cmd) (func(), error) {
	job, err := killOnCloseJob()
	if err != nil {
		return nil, err
	}
	release := func() { closeHandle(job) }
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createSuspended}
	cmd.Cancel = func() error {
		if r, _, err := procTerminateJobObject.Call(uintptr(job), 1); r == 0 {
			return fmt.Errorf("terminating the job of process %d: %w", cmd.Process.Pid, err)
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		release()
		return nil, err
	}
	if err := adopt(job, cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		release()
		return nil, err
	}
	return release, nil
}

func closeHandle(h syscall.Handle) { _ = syscall.CloseHandle(h) }

func killOnCloseJob() (syscall.Handle, error) {
	r, _, err := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		return 0, fmt.Errorf("creating a job object: %w", err)
	}
	job := syscall.Handle(r)
	limits := jobExtendedLimits{basic: jobBasicLimits{limitFlags: jobLimitKillOnJobClose}}
	if r, _, err := procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInfo, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); r == 0 { //nolint:gosec // G103: SetInformationJobObject reads the limits struct through this pointer for the duration of the call
		closeHandle(job)
		return 0, fmt.Errorf("making the job kill its processes when closed: %w", err)
	}
	return job, nil
}

func adopt(job syscall.Handle, pid int) error {
	if pid < 0 || pid > math.MaxUint32 {
		return fmt.Errorf("process id %d is not a Windows process id", pid)
	}
	id := uint32(pid)
	process, err := syscall.OpenProcess(processSetQuota|syscall.PROCESS_TERMINATE, false, id)
	if err != nil {
		return fmt.Errorf("opening process %d: %w", id, err)
	}
	defer closeHandle(process)
	if r, _, err := procAssignProcessToJob.Call(uintptr(job), uintptr(process)); r == 0 {
		return fmt.Errorf("assigning process %d to its job: %w", id, err)
	}
	return resumeThreads(id)
}

func resumeThreads(pid uint32) error {
	snapshot, err := syscall.CreateToolhelp32Snapshot(snapshotThreads, 0)
	if err != nil {
		return fmt.Errorf("listing the threads of process %d: %w", pid, err)
	}
	defer closeHandle(snapshot)
	entry := threadEntry{size: uint32(unsafe.Sizeof(threadEntry{}))}
	resumed := 0
	r, _, err := procThread32First.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))               //nolint:gosec // G103: Thread32First fills the entry through this pointer for the duration of the call
	for ; r != 0; r, _, err = procThread32Next.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry))) { //nolint:gosec // G103: Thread32Next fills the entry through this pointer for the duration of the call
		if entry.ownerProcessID != pid {
			continue
		}
		if err := resumeThread(entry.threadID); err != nil {
			return fmt.Errorf("resuming process %d: %w", pid, err)
		}
		resumed++
	}
	if !errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("listing the threads of process %d: %w", pid, err)
	}
	if resumed == 0 {
		return fmt.Errorf("resuming process %d: %w", pid, os.ErrProcessDone)
	}
	return nil
}

func resumeThread(id uint32) error {
	r, _, err := procOpenThread.Call(threadSuspendResume, 0, uintptr(id))
	if r == 0 {
		return fmt.Errorf("opening thread %d: %w", id, err)
	}
	thread := syscall.Handle(r)
	defer closeHandle(thread)
	if r, _, err := procResumeThread.Call(uintptr(thread)); r == resumeFailed {
		return fmt.Errorf("resuming thread %d: %w", id, err)
	}
	return nil
}
