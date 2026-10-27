//go:build windows

package install

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows service is managed through the Service Control Manager API.
//
// It used to be managed by shelling out to sc.exe, and that is a parsing
// exercise with no reward: sc.exe reads its own options (`binPath=`, `start=`,
// `display=`) out of the raw command line, so Go's argument quoting hides the
// option name; quoting the path alone leaves the agent's arguments outside the
// value; a value with a space has to travel in the same token as its option.
// Four command-line shapes were tried on the CI runner and every one answered
// 1639 with a usage dump — a number that names nothing. The API takes the same
// fields as arguments and returns the system's real error, so the installer
// uses that: one less parser, and a failure that says what failed.

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func openServiceManager(access uint32) (windows.Handle, error) {
	scm, err := windows.OpenSCManager(nil, nil, access)
	if err != nil {
		return 0, fmt.Errorf("open the service control manager: %w", err)
	}
	return scm, nil
}

// createService registers the service; the caller closes the handle.
func createService(name, display, binPath string) (windows.Handle, error) {
	scm, err := openServiceManager(windows.SC_MANAGER_CREATE_SERVICE)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(scm)

	svc, err := windows.CreateService(
		scm,
		utf16(name), utf16(display),
		windows.SERVICE_ALL_ACCESS,
		windows.SERVICE_WIN32_OWN_PROCESS,
		windows.SERVICE_AUTO_START,
		windows.SERVICE_ERROR_NORMAL,
		utf16(binPath),
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		return 0, fmt.Errorf("create service %q (running %s): %w", name, binPath, err)
	}
	return svc, nil
}

func openService(name string, access uint32) (windows.Handle, error) {
	scm, err := openServiceManager(windows.GENERIC_READ)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(scm)

	svc, err := windows.OpenService(scm, utf16(name), access)
	if err != nil {
		return 0, fmt.Errorf("open service %q: %w", name, err)
	}
	return svc, nil
}

func startService(name string) error {
	svc, err := openService(name, windows.SERVICE_START)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(svc)
	return windows.StartService(svc, 0, nil)
}

// stopService asks the service to stop. A service that is already stopped is
// not a failure — that is the state we asked for.
func stopService(name string) error {
	svc, err := openService(name, windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(svc)

	var status windows.SERVICE_STATUS
	err = windows.ControlService(svc, windows.SERVICE_CONTROL_STOP, &status)
	if err == windows.ERROR_SERVICE_NOT_ACTIVE {
		return nil
	}
	return err
}

func removeService(name string) error {
	svc, err := openService(name, windows.DELETE)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(svc)
	return windows.DeleteService(svc)
}

func serviceExists(name string) (bool, error) {
	scm, err := openServiceManager(windows.GENERIC_READ)
	if err != nil {
		return false, err
	}
	defer windows.CloseServiceHandle(scm)

	svc, err := windows.OpenService(scm, utf16(name), windows.GENERIC_READ)
	if err == nil {
		windows.CloseServiceHandle(svc)
		return true, nil
	}
	if err == windows.ERROR_SERVICE_DOES_NOT_EXIST {
		return false, nil
	}
	return false, err
}

// setServiceRecovery restarts the agent on the first three failures
// (5 s / 10 s / 30 s) and resets the failure count after a day of stable
// uptime, so a crashed agent comes back without anyone logging in.
func setServiceRecovery(name string) error {
	svc, err := openService(name, windows.SERVICE_CHANGE_CONFIG|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(svc)

	actions := []windows.SC_ACTION{
		{Type: windows.SC_ACTION_RESTART, Delay: 5000},
		{Type: windows.SC_ACTION_RESTART, Delay: 10000},
		{Type: windows.SC_ACTION_RESTART, Delay: 30000},
	}
	cfg := windows.SERVICE_FAILURE_ACTIONS{
		ResetPeriod:  86400,
		ActionsCount: uint32(len(actions)),
		Actions:      &actions[0],
	}
	return windows.ChangeServiceConfig2(svc, windows.SERVICE_CONFIG_FAILURE_ACTIONS,
		(*byte)(unsafe.Pointer(&cfg)))
}
