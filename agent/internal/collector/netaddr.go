package collector

import (
	"net"
	"sort"
)

// maxLocalIPs bounds the list an agent reports; a PC with more than this many
// usable IPv4 addresses is a virtualisation host, not a workstation.
const maxLocalIPs = 8

// LocalIPv4s returns the machine's own unicast IPv4 addresses on interfaces that
// are up and not loopback. Link-local (169.254/16, APIPA) addresses are skipped:
// they mean "no DHCP lease", which is not an address anyone can reach the PC on.
//
// Privacy: this is the machine's identity on the LAN, on a par with hostname.
// It reads interface configuration only — never sockets, peers or traffic.
func LocalIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 4)
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() {
				continue
			}
			s := ip4.String()
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	if len(out) > maxLocalIPs {
		out = out[:maxLocalIPs]
	}
	return out
}
