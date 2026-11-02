//go:build windows

package winjob

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job is an owning handle to a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
type Job struct {
	handle windows.Handle
}

// New creates the job object used for the whole supervisor lifetime. One job is
// shared by every instance so that closing a single handle terminates all model
// processes at once.
func New() (*Job, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	return &Job{handle: handle}, nil
}

// AssignPID puts an already-started process into the job.
//
// The assignment happens immediately after Start rather than under
// CREATE_SUSPENDED, because os/exec does not expose the primary thread handle
// needed to resume it. The residual window is microseconds and llama-server.exe
// spawns no children, so no grandchild can escape before it is assigned. This
// trade-off is deliberate, not an oversight.
func (j *Job) AssignPID(pid int) error {
	if j == nil || j.handle == 0 {
		return ErrUnsupported
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(j.handle, process); err != nil {
		return fmt.Errorf("assign process %d to job: %w", pid, err)
	}
	return nil
}

// Close releases the job handle. With KILL_ON_JOB_CLOSE set, every process still
// in the job is terminated by the kernel at this point.
func (j *Job) Close() error {
	if j == nil || j.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(j.handle)
	j.handle = 0
	if err != nil {
		return fmt.Errorf("close job object: %w", err)
	}
	return nil
}
