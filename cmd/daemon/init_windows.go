package daemon

// https://github.com/kubernetes/kubernetes/blob/master/cmd/kubelet/app/init_windows.go

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createWindowsJobObject creates a new Job Object
// (https://docs.microsoft.com/en-us/windows/win32/procthread/job-objects),
// and specifies the priority class for the job object to the specified value.
// A job object is used here so that any spawned processes such as powershell or
// wmic are created at the specified thread priority class.
// Running Kube-OVN with above normal / high priority  can help improve
// responsiveness on machines with high CPU utilization.
func createWindowsJobObject(pc uint32) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("windows.CreateJobObject failed: %v", err)
	}
	limitInfo := windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
		LimitFlags:    windows.JOB_OBJECT_LIMIT_PRIORITY_CLASS,
		PriorityClass: pc,
	}
	if _, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectBasicLimitInformation,
		uintptr(unsafe.Pointer(&limitInfo)),
		uint32(unsafe.Sizeof(limitInfo))); err != nil {
		return windows.InvalidHandle, fmt.Errorf("windows.SetInformationJobObject failed: %v", err)
	}
	return job, nil
}

func initForOS() error {
	job, err := createWindowsJobObject(uint32(windows.NORMAL_PRIORITY_CLASS))
	if err != nil {
		return err
	}
	if err = windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return fmt.Errorf("windows.AssignProcessToJobObject failed: %v", err)
	}

	return nil
}
