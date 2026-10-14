//go:build linux

package collector

import (
	"strings"

	"github.com/shirou/gopsutil/v4/host"
)

// platformMachine returns OS name, kernel build and kind for Linux. Linux hosts
// are the Ubuntu server monitoring itself; there is no AD domain.
func platformMachine() (osName, osBuild, domain, kind string) {
	kind = "linux"
	if plat, _, ver, err := host.PlatformInformation(); err == nil {
		osName = strings.TrimSpace(plat + " " + ver)
	}
	if info, err := host.Info(); err == nil {
		osBuild = info.KernelVersion
	}
	return osName, osBuild, "", kind
}

// pseudoFS lists non-physical filesystem types to exclude from disk usage.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true,
	"aufs": true, "proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
	"ramfs": true, "devpts": true, "mqueue": true, "debugfs": true,
	"tracefs": true, "securityfs": true, "pstore": true, "autofs": true,
	"binfmt_misc": true, "configfs": true, "fusectl": true, "nsfs": true,
	"hugetlbfs": true, "efivarfs": true, "bpf": true, "none": true,
}

// isFixedDisk reports whether a partition is a real local filesystem.
func isFixedDisk(mount, fstype, _ /*opts*/ string) bool {
	if pseudoFS[strings.ToLower(fstype)] {
		return false
	}
	for _, pfx := range []string{"/proc", "/sys", "/dev", "/run", "/snap"} {
		if mount == pfx || strings.HasPrefix(mount, pfx+"/") {
			return false
		}
	}
	return true
}

// normalizeMount is a no-op on Linux.
func normalizeMount(m string) string { return m }
