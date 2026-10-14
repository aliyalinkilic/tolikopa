package ingest

import (
	"strings"
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

func TestClipStringsBoundsFreeText(t *testing.T) {
	long := strings.Repeat("ş", maxTextLen+50) // multi-byte: clipped by runes, never mid-rune
	env := model.Envelope{
		Machine:    model.Machine{Hostname: long, OS: long},
		Samples:    []model.Sample{{LogonUser: long, Disks: []model.Disk{{Mount: long}}, Processes: []model.Process{{Name: long}}}},
		Events:     []model.Event{{Kind: "app_crash", ProcessName: long, Detail: map[string]string{"code": long, strings.Repeat("k", 65): "x"}}},
		DiskHealth: []model.DiskHealth{{Disk: long}},
		AppFocus:   []model.AppFocus{{Name: long, Seconds: 1}},
	}
	clipStrings(&env)
	for name, s := range map[string]string{
		"hostname": env.Machine.Hostname, "os": env.Machine.OS, "user": env.Samples[0].LogonUser,
		"mount": env.Samples[0].Disks[0].Mount, "process": env.Samples[0].Processes[0].Name,
		"event process": env.Events[0].ProcessName, "event detail": env.Events[0].Detail["code"],
		"disk": env.DiskHealth[0].Disk, "app": env.AppFocus[0].Name,
	} {
		if n := len([]rune(s)); n != maxTextLen {
			t.Errorf("%s: %d runes, want %d", name, n, maxTextLen)
		}
		if !strings.HasPrefix(long, s) {
			t.Errorf("%s: not a clean prefix", name)
		}
	}
	if len(env.Events[0].Detail) != 1 {
		t.Errorf("over-long detail key kept: %v", env.Events[0].Detail)
	}
	if clip("PC-001") != "PC-001" {
		t.Error("short value changed")
	}
}
