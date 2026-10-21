package alerts

import (
	"fmt"
	"strings"
)

// This file is the server's whole message table. The panel localises alerts on
// its own from `rule` + `params`; the server needs text only for (a) the stored
// fallback `alert.message` and (b) alert e-mails, both rendered in the instance
// default language. Both languages must stay complete — TestMessagesComplete
// fails if a rule is missing one.

// Languages the server can render.
const (
	LangTR = "tr"
	LangEN = "en"
)

// NormalizeLang maps anything unrecognised to Turkish, the instance default.
func NormalizeLang(s string) string {
	if s == LangEN {
		return LangEN
	}
	return LangTR
}

// Rules is every alert rule id the engine evaluates.
var Rules = []string{"offline", "cpu_high", "mem_high", "disk_low", "smart_fail", "crash_storm"}

// Params is the structured payload of an alert (stored in alert.detail, exposed
// as AlertDTO.Params). Values arrive either as Go ints/floats (fresh from an
// evaluator) or as float64 (decoded from jsonb), so read them via num/str.
type Params = map[string]any

func num(p Params, key string) float64 {
	switch v := p[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	}
	return 0
}

func str(p Params, key string) string {
	s, _ := p[key].(string)
	return s
}

type renderer func(p Params) string

var messages = map[string]map[string]renderer{
	// offline: params.kind distinguishes work-hours devices (docs/API.md §6.1).
	"offline": {
		LangTR: func(p Params) string {
			switch str(p, "kind") {
			case "not_started":
				return fmt.Sprintf("%s bugün hiç açılmadı", str(p, "hostname"))
			case "went_offline":
				return fmt.Sprintf("%s gün içinde çevrimdışı oldu (%d dk görülmedi)", str(p, "hostname"), int(num(p, "minutes")))
			case "stale":
				return fmt.Sprintf("%s uzun süredir sessiz (%d gündür görülmedi) — kullanılmıyorsa arşivleyin", str(p, "hostname"), int(num(p, "days")))
			}
			return fmt.Sprintf("%s çevrimdışı (%d dk görülmedi)", str(p, "hostname"), int(num(p, "minutes")))
		},
		LangEN: func(p Params) string {
			switch str(p, "kind") {
			case "not_started":
				return fmt.Sprintf("%s has not been switched on today", str(p, "hostname"))
			case "went_offline":
				return fmt.Sprintf("%s went offline during the day (not seen for %d min)", str(p, "hostname"), int(num(p, "minutes")))
			case "stale":
				return fmt.Sprintf("%s has been silent for a long time (not seen for %d days) — archive it if it is no longer in use", str(p, "hostname"), int(num(p, "days")))
			}
			return fmt.Sprintf("%s is offline (not seen for %d min)", str(p, "hostname"), int(num(p, "minutes")))
		},
	},
	"cpu_high": {
		LangTR: func(p Params) string {
			return fmt.Sprintf("%s CPU yüksek: %%%.0f", str(p, "hostname"), num(p, "value"))
		},
		LangEN: func(p Params) string {
			return fmt.Sprintf("%s CPU usage is high: %.0f%%", str(p, "hostname"), num(p, "value"))
		},
	},
	"mem_high": {
		LangTR: func(p Params) string {
			return fmt.Sprintf("%s bellek yüksek: %%%.0f", str(p, "hostname"), num(p, "value"))
		},
		LangEN: func(p Params) string {
			return fmt.Sprintf("%s memory usage is high: %.0f%%", str(p, "hostname"), num(p, "value"))
		},
	},
	"disk_low": {
		LangTR: func(p Params) string {
			return fmt.Sprintf("%s %s diskinde boş alan az: %%%.1f", str(p, "hostname"), str(p, "mount"), num(p, "free_pct"))
		},
		LangEN: func(p Params) string {
			return fmt.Sprintf("%s is low on free space on %s: %.1f%%", str(p, "hostname"), str(p, "mount"), num(p, "free_pct"))
		},
	},
	"smart_fail": {
		LangTR: func(p Params) string {
			return fmt.Sprintf("%s %s diski SMART arızası bildiriyor", str(p, "hostname"), str(p, "disk"))
		},
		LangEN: func(p Params) string {
			return fmt.Sprintf("%s disk %s reports a SMART failure", str(p, "hostname"), str(p, "disk"))
		},
	},
	"crash_storm": {
		LangTR: func(p Params) string {
			if app := str(p, "app"); app != "" {
				return fmt.Sprintf("%s son %d dk'da %d çökme (en sık: %s)", str(p, "hostname"), int(num(p, "window_minutes")), int(num(p, "count")), app)
			}
			return fmt.Sprintf("%s son %d dk'da %d çökme", str(p, "hostname"), int(num(p, "window_minutes")), int(num(p, "count")))
		},
		LangEN: func(p Params) string {
			if app := str(p, "app"); app != "" {
				return fmt.Sprintf("%s had %d crashes in the last %d min (mostly %s)", str(p, "hostname"), int(num(p, "count")), int(num(p, "window_minutes")), app)
			}
			return fmt.Sprintf("%s had %d crashes in the last %d min", str(p, "hostname"), int(num(p, "count")), int(num(p, "window_minutes")))
		},
	},
}

// RenderMessage renders an alert's one-line text in lang. An unknown rule (a
// newer row read by an older binary) degrades to "<rule>: <hostname>".
func RenderMessage(lang, rule string, p Params) string {
	if byLang, ok := messages[rule]; ok {
		if fn, ok := byLang[NormalizeLang(lang)]; ok {
			return oneLine(fn(p))
		}
	}
	return oneLine(strings.TrimSpace(rule + ": " + str(p, "hostname")))
}

// ruleNames are the human names of the rules, used in e-mails.
var ruleNames = map[string]map[string]string{
	"offline":     {LangTR: "Çevrimdışı makine", LangEN: "Machine offline"},
	"cpu_high":    {LangTR: "Yüksek CPU kullanımı", LangEN: "High CPU usage"},
	"mem_high":    {LangTR: "Yüksek bellek kullanımı", LangEN: "High memory usage"},
	"disk_low":    {LangTR: "Düşük disk alanı", LangEN: "Low disk space"},
	"smart_fail":  {LangTR: "Disk SMART arızası", LangEN: "Disk SMART failure"},
	"crash_storm": {LangTR: "Çökme fırtınası", LangEN: "Crash storm"},
}

// RuleName returns the rule's display name in lang, or the id if unknown.
func RuleName(lang, rule string) string {
	if n := ruleNames[rule][NormalizeLang(lang)]; n != "" {
		return n
	}
	return rule
}

// emailText is the chrome of the critical-alert e-mail.
type emailText struct {
	subject string // one %s: the alert message
	intro   string
	machine string
	rule    string
	message string
	time    string
}

var emailTexts = map[string]emailText{
	LangTR: {
		subject: "[Tolikopa] Kritik alarm: %s",
		intro:   "Kritik bir alarm açıldı.",
		machine: "Makine",
		rule:    "Kural",
		message: "Mesaj",
		time:    "Zaman",
	},
	LangEN: {
		subject: "[Tolikopa] Critical alert: %s",
		intro:   "A critical alert has opened.",
		machine: "Machine",
		rule:    "Rule",
		message: "Message",
		time:    "Time",
	},
}

// oneLine removes line breaks and other control characters. Hostnames come from
// agents; a CR/LF surviving into an e-mail Subject would be header injection.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// CompleteParams returns the params object the API exposes for an alert row.
// Rows written by this version already carry everything; for older rows it
// fills in what can be derived (hostname from the join, minutes from
// age_seconds, units …) so the client can render them too. `threshold` cannot
// be reconstructed and is simply absent on such rows. The result is never nil.
func CompleteParams(rule string, stored Params, hostname string, value *float64) Params {
	p := make(Params, len(stored)+4)
	for k, v := range stored {
		p[k] = v
	}
	if _, ok := p["hostname"]; !ok && hostname != "" {
		p["hostname"] = hostname
	}
	setDefault := func(k string, v any) {
		if _, ok := p[k]; !ok {
			p[k] = v
		}
	}
	switch rule {
	case "offline":
		if _, ok := p["age_seconds"]; !ok && value != nil {
			p["age_seconds"] = int(*value)
		}
		if _, ok := p["age_seconds"]; ok {
			setDefault("minutes", int(num(p, "age_seconds")/60))
		}
		setDefault("unit", "s")
	case "cpu_high", "mem_high":
		if value != nil {
			setDefault("value", *value)
		}
		setDefault("unit", "%")
	case "disk_low":
		if value != nil {
			setDefault("free_pct", *value)
		}
		setDefault("unit", "%")
	case "crash_storm":
		if value != nil {
			setDefault("count", int(*value))
		}
		if _, ok := p["window_seconds"]; ok {
			setDefault("window_minutes", int(num(p, "window_seconds")/60))
		}
	}
	return p
}
