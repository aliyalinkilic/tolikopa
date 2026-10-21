package ingest

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func testCats() *Categories {
	c := &Categories{}
	c.load([]catRule{
		{id: 1, category: "browser", matchType: "exact", matchValue: "chrome", priority: 10},
		{id: 2, category: "dev", matchType: "prefix", matchValue: "python", priority: 50},
		{id: 3, category: "utility", matchType: "prefix", matchValue: "hp", priority: 60},
		{id: 4, category: "security", matchType: "prefix", matchValue: "hpsec", priority: 60},
		{id: 5, category: "system", matchType: "glob", matchValue: "kworker*", priority: 90},
		{id: 6, category: "utility", matchType: "glob", matchValue: "*updater", priority: 90},
		{id: 7, category: "browser", matchType: "prefix", matchValue: "chrome", priority: 50},
	})
	return c
}

func TestResolveOrder(t *testing.T) {
	c := testCats()
	cases := []struct {
		name string
		want string
	}{
		{`C:\Program Files\Google\Chrome.EXE`, "browser"}, // exact, basename, .exe stripped
		{"python3.12", "dev"},                             // prefix
		{"hpsecure", "security"},                          // same priority: longer prefix wins
		{"hpprint", "utility"},
		{"kworker/0:1", "system"},    // kernel thread: part before the slash
		{"kworker-events", "system"}, // glob
		{"logioptionsplus_updater", "utility"},
		{"notepad", ""},
	}
	for _, tc := range cases {
		_, got, ok := c.Resolve(tc.name)
		if tc.want == "" {
			if ok {
				t.Errorf("Resolve(%q) = %q, want unclassified", tc.name, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("Resolve(%q) = %q,%v, want %q", tc.name, got, ok, tc.want)
		}
	}
}

func TestExplainListsShadowedRules(t *testing.T) {
	c := testCats()
	norm, m := c.Explain("Chrome.exe")
	if norm != "chrome" {
		t.Fatalf("norm = %q", norm)
	}
	if len(m) != 2 || m[0].ID != 1 || m[1].ID != 7 {
		t.Fatalf("Explain(chrome) = %+v, want exact rule 1 then shadowed prefix 7", m)
	}
	// Explain's first match must always agree with Resolve.
	for _, n := range []string{"hpsecure", "python", "kworker-x", "chromium", "zzz"} {
		id, _, ok := c.Resolve(n)
		_, m := c.Explain(n)
		if ok != (len(m) > 0) || (ok && m[0].ID != id) {
			t.Errorf("Explain(%q) disagrees with Resolve: %+v vs %d,%v", n, m, id, ok)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Chrome.EXE":                        "chrome",
		`C:\Program Files\App\Tool.exe`:     "tool",
		"/usr/lib/systemd/systemd-journald": "systemd-journald",
		"kworker/0:1-events":                "kworker",
		"ksoftirqd/3":                       "ksoftirqd",
		"irq/31-vmw_vmci":                   "irq",
		"  Memory Compression ":             "memory compression",
		"":                                  "",
		`relative\dir\name.exe`:             "name",
	}
	for in, want := range cases {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// seedRules loads the rule tuples the seed migrations insert, so the test sees
// the shipped classification exactly as a fresh install would (the 0018 seed
// corrections are UPDATEs and are applied by hand below).
func seedRules(t *testing.T) []catRule {
	t.Helper()
	tuple := regexp.MustCompile(`\('([a-z]+)','(exact|prefix|glob)','([^']+)',(\d+)`)
	var rules []catRule
	for _, f := range []string{"0004_seed.sql", "0018_category_rules.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "db", "migrations", f))
		if err != nil {
			t.Fatal(err)
		}
		// Only the INSERT statements carry rules (0018's builtin-marking UPDATE
		// lists the 0004 tuples again).
		for _, stmt := range strings.Split(string(raw), ";") {
			if !strings.Contains(stmt, "INSERT INTO process_category") {
				continue
			}
			for _, m := range tuple.FindAllStringSubmatch(stmt, -1) {
				p, _ := strconv.Atoi(m[4])
				cat := m[1]
				switch m[3] {
				case "msmpeng", "securityhealthservice":
					cat = "security"
				case "cmd", "powershell":
					cat = "system"
				}
				rules = append(rules, catRule{id: int16(len(rules) + 1), category: cat, matchType: m[2], matchValue: m[3], priority: p})
			}
		}
	}
	if len(rules) < 600 { // 0014 only repeats a 0004 row
		t.Fatalf("parsed only %d seed rules", len(rules))
	}
	return rules
}

func TestSeedRulesClassifyCommonProcesses(t *testing.T) {
	rules := seedRules(t)
	seen := map[string]bool{}
	for _, r := range rules {
		k := r.matchType + ":" + r.matchValue
		if seen[k] {
			t.Errorf("duplicate seed rule %s", k)
		}
		seen[k] = true
		if r.matchValue != strings.ToLower(strings.TrimSpace(r.matchValue)) || strings.HasSuffix(r.matchValue, ".exe") {
			t.Errorf("seed value %q is not normalised", r.matchValue)
		}
	}
	c := &Categories{}
	c.load(rules)
	for name, want := range map[string]string{
		"chrome.exe":              "browser",
		"MsEdgeWebView2.exe":      "system",
		"MsMpEng.exe":             "security",
		"mcshield":                "security",
		"FortiTray.exe":           "security",
		"powershell.exe":          "system",
		"pwsh":                    "dev",
		"TeamViewer_Service.exe":  "remote",
		"rustdesk":                "remote",
		"WhatsApp.Root.exe":       "comms",
		"ms-teams":                "comms",
		"GoogleUpdate.exe":        "utility",
		"logioptionsplus_updater": "utility",
		"HPSvcsScan.exe":          "utility",
		"OneDrive.exe":            "utility",
		"kworker/0:1-events":      "system",
		"systemd-journald":        "system",
		"rcu_tasks_kthread":       "system",
		"Photoshop.exe":           "media",
		"notepad++.exe":           "dev",
		"olk.exe":                 "office",
	} {
		if _, got, _ := c.Resolve(name); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", name, got, want)
		}
	}
}
