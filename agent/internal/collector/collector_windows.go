//go:build windows

package collector

import (
	"strconv"
	"strings"
	"unsafe"

	"github.com/shirou/gopsutil/v4/host"
	"golang.org/x/sys/windows"
)

// platformMachine returns OS name, OS build, AD domain and kind for Windows.
func platformMachine() (osName, osBuild, domain, kind string) {
	kind = "windows"
	if plat, _, _, err := host.PlatformInformation(); err == nil && plat != "" {
		osName = strings.TrimPrefix(plat, "Microsoft ")
	}
	if v := windows.RtlGetVersion(); v != nil {
		osBuild = strconv.FormatUint(uint64(v.BuildNumber), 10)
	}
	domain = joinDomain()
	return
}

// joinDomain returns the AD domain (NetBIOS) name, or "" for a workgroup machine.
func joinDomain() string {
	var namePtr *uint16
	var bufType uint32
	if err := windows.NetGetJoinInformation(nil, &namePtr, &bufType); err != nil {
		return ""
	}
	defer windows.NetApiBufferFree((*byte)(unsafe.Pointer(namePtr)))
	const netSetupDomainName = 3 // NETSETUP_JOIN_STATUS: NetSetupDomainName
	if bufType != netSetupDomainName {
		return ""
	}
	return windows.UTF16PtrToString(namePtr)
}

// isFixedDisk reports whether mount is a local fixed drive (DRIVE_FIXED).
func isFixedDisk(mount, _ /*fstype*/, _ /*opts*/ string) bool {
	root := mount
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false
	}
	return windows.GetDriveType(p) == windows.DRIVE_FIXED
}

// normalizeMount trims the trailing separator so "C:\" becomes "C:".
func normalizeMount(m string) string {
	return strings.TrimSuffix(m, `\`)
}
