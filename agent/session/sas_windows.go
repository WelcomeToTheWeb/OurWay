//go:build windows

package session

import (
	"fmt"
	"log"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

var procSendSAS = syscall.NewLazyDLL("sas.dll").NewProc("SendSAS")

const sasPolicyKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`

// ensureSASPolicy makes sure Windows lets services generate the Secure
// Attention Sequence. SendSAS is a no-op unless the SoftwareSASGeneration
// policy has the "services" bit (1); 2 is ease-of-access apps, 3 both.
func ensureSASPolicy() error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, sasPolicyKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open policy key: %w", err)
	}
	defer k.Close()
	cur, _, _ := k.GetIntegerValue("SoftwareSASGeneration")
	if cur&1 != 0 {
		return nil
	}
	log.Printf("session: enabling SoftwareSASGeneration for services (was %d)", cur)
	return k.SetDWordValue("SoftwareSASGeneration", uint32(cur|1))
}

// SendSAS raises Ctrl+Alt+Del on the console. It must be called from a
// service running as SYSTEM (the agent), not from the user-session exe.
func SendSAS() error {
	if err := ensureSASPolicy(); err != nil {
		return err
	}
	if err := procSendSAS.Find(); err != nil {
		return fmt.Errorf("sas.dll: %w", err)
	}
	procSendSAS.Call(0) // AsUser = FALSE: caller is a service
	return nil
}
