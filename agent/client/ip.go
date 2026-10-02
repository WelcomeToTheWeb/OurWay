package client

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
)

// publicIPService is the best-effort endpoint used to learn this host's
// public (NAT-egress) address at startup. Failure is non-fatal: the
// field is simply left empty.
const publicIPService = "https://api.ipify.org"

// PrivateIP returns the first non-loopback IPv4 address assigned to a
// running interface, or "" if none is found. It is evaluated on every
// call (cheap: no sockets are opened) so a changed network shows up in
// the next heartbeat.
func PrivateIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			return ip4.String()
		}
	}
	return ""
}

// FetchPublicIP resolves this host's public IP via publicIPService.
// It is best-effort with a short timeout; on any failure it returns ""
// so callers can keep the previously known value.
func FetchPublicIP(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, "GET", publicIPService, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}
