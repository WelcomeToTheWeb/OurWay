//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const protoKey = `Software\Classes\ourway`

// registerProtocol points the per-user ourway:// handler at this
// executable. It needs no elevation (HKCU) and is idempotent, so it is
// safe to run at every start, which also repairs a moved install.
func registerProtocol() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	want := fmt.Sprintf(`"%s" "%%1"`, exe)

	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, protoKey+`\shell\open\command`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer cmd.Close()
	if cur, _, err := cmd.GetStringValue(""); err == nil && cur == want {
		return nil
	}
	root, _, err := registry.CreateKey(registry.CURRENT_USER, protoKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:OurWay Remote Session"); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	return cmd.SetStringValue("", want)
}

// unregisterProtocol removes the handler.
func unregisterProtocol() error {
	for _, k := range []string{protoKey + `\shell\open\command`, protoKey + `\shell\open`, protoKey + `\shell`, protoKey} {
		if err := registry.DeleteKey(registry.CURRENT_USER, k); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	return nil
}
