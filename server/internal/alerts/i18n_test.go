package alerts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// sampleParams are params as each evaluator produces them.
var sampleParams = map[string]Params{
	"offline":     {"hostname": "PC-01", "age_seconds": 1860, "minutes": 31, "threshold": 1800.0, "unit": "s"},
	"cpu_high":    {"hostname": "PC-01", "value": 96.4, "threshold": 95.0, "unit": "%", "window_seconds": 300},
	"mem_high":    {"hostname": "PC-01", "value": 91.2, "threshold": 90.0, "unit": "%", "window_seconds": 300},
	"disk_low":    {"hostname": "PC-01", "mount": "C:", "free_pct": 4.25, "threshold": 5.0, "unit": "%"},
	"smart_fail":  {"hostname": "PC-01", "disk": "Disk0", "smart_ok": false, "predict_fail": true},
	"crash_storm": {"hostname": "PC-01", "count": 7, "window_seconds": 600, "window_minutes": 10, "threshold": 5.0},
}

// Both languages must be complete for every rule the engine evaluates, and the
// rendering must not depend on whether params came from Go or from jsonb.
func TestMessagesComplete(t *testing.T) {
	for _, rule := range Rules {
		p, ok := sampleParams[rule]
		if !ok {
			t.Fatalf("no sample params for rule %s", rule)
		}
		raw, _ := json.Marshal(p)
		var fromDB Params
		_ = json.Unmarshal(raw, &fromDB)

		tr, en := RenderMessage(LangTR, rule, p), RenderMessage(LangEN, rule, p)
		if tr == "" || en == "" || tr == en {
			t.Errorf("%s: tr=%q en=%q", rule, tr, en)
		}
		for lang, msg := range map[string]string{LangTR: tr, LangEN: en} {
			if !strings.Contains(msg, "PC-01") {
				t.Errorf("%s/%s lacks the hostname: %q", rule, lang, msg)
			}
			if strings.Contains(msg, "%!") {
				t.Errorf("%s/%s has a formatting error: %q", rule, lang, msg)
			}
			if got := RenderMessage(lang, rule, fromDB); got != msg {
				t.Errorf("%s/%s differs after a jsonb round trip: %q vs %q", rule, lang, got, msg)
			}
			if RuleName(lang, rule) == rule {
				t.Errorf("%s/%s has no display name", rule, lang)
			}
		}
	}
	if len(messages) != len(Rules) || len(ruleNames) != len(Rules) {
		t.Errorf("tables out of sync: %d messages, %d names, %d rules", len(messages), len(ruleNames), len(Rules))
	}
	for _, lang := range []string{LangTR, LangEN} {
		e := emailTexts[lang]
		if e.subject == "" || e.intro == "" || e.machine == "" || e.rule == "" || e.message == "" || e.time == "" {
			t.Errorf("e-mail text incomplete for %s: %+v", lang, e)
		}
	}
}

func TestRenderMessageValues(t *testing.T) {
	if got, want := RenderMessage("tr", "offline", sampleParams["offline"]), "PC-01 çevrimdışı (31 dk görülmedi)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := RenderMessage("en", "crash_storm", sampleParams["crash_storm"]), "PC-01 had 7 crashes in the last 10 min"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := RenderMessage("en", "disk_low", sampleParams["disk_low"]), "PC-01 is low on free space on C:: 4.2%"; got != want && got != "PC-01 is low on free space on C:: 4.3%" {
		t.Errorf("got %q, want %q", got, want)
	}
	// Unknown language falls back to Turkish; unknown rule degrades gracefully.
	if RenderMessage("de", "cpu_high", sampleParams["cpu_high"]) != RenderMessage("tr", "cpu_high", sampleParams["cpu_high"]) {
		t.Error("unknown language did not fall back to tr")
	}
	if got := RenderMessage("en", "future_rule", Params{"hostname": "PC-01"}); got != "future_rule: PC-01" {
		t.Errorf("unknown rule: %q", got)
	}
}

// Rows written before alerts carried full params must still be renderable.
func TestCompleteParamsBackfillsOldRows(t *testing.T) {
	v := 1860.0
	p := CompleteParams("offline", Params{"age_seconds": 1860.0}, "PC-01", &v)
	if p["hostname"] != "PC-01" || p["minutes"] != 31 || p["unit"] != "s" {
		t.Errorf("offline: %v", p)
	}
	if got := RenderMessage("en", "offline", p); got != "PC-01 is offline (not seen for 31 min)" {
		t.Errorf("old offline row renders as %q", got)
	}

	c := 7.0
	p = CompleteParams("crash_storm", Params{"count": 7.0, "window_seconds": 600.0}, "PC-01", &c)
	if p["window_minutes"] != 10 {
		t.Errorf("crash_storm: %v", p)
	}

	f := 4.2
	p = CompleteParams("disk_low", Params{"mount": "C:"}, "PC-01", &f)
	if p["free_pct"] != 4.2 || p["unit"] != "%" {
		t.Errorf("disk_low: %v", p)
	}
	if _, has := p["threshold"]; has {
		t.Error("threshold invented for an old row")
	}

	// Never nil (the DTO promises an object), never mutates the input, and a
	// stored hostname wins over the joined one.
	if CompleteParams("smart_fail", nil, "", nil) == nil {
		t.Error("nil params")
	}
	stored := Params{"hostname": "OLD-NAME", "value": 50.0}
	p = CompleteParams("cpu_high", stored, "NEW-NAME", nil)
	if p["hostname"] != "OLD-NAME" || len(stored) != 2 {
		t.Errorf("stored params not respected: %v / %v", p, stored)
	}
}

// Hostnames are agent-supplied: a line break must never reach the Subject header.
func TestEmailIsLocalisedAndHeaderSafe(t *testing.T) {
	a := model.AlertDTO{
		MachineID: "m-1", Hostname: "PC-01\r\nBcc: someone@corp.example.com", Rule: "offline",
		Params:   Params{"hostname": "PC-01\r\nBcc: someone@corp.example.com", "minutes": 31},
		OpenedAt: time.Unix(1_700_000_000, 0),
	}
	for lang, want := range map[string]string{"tr": "Kritik alarm", "en": "Critical alert"} {
		subject, body := buildEmail(a, lang)
		if strings.ContainsAny(subject, "\r\n") {
			t.Errorf("%s: subject contains a line break: %q", lang, subject)
		}
		if !strings.Contains(subject, want) {
			t.Errorf("%s: subject %q lacks %q", lang, subject, want)
		}
		if strings.Contains(body, "\nBcc:") {
			t.Errorf("%s: injected header line in body: %q", lang, body)
		}
		if !strings.Contains(body, RuleName(lang, "offline")) {
			t.Errorf("%s: body lacks the rule name", lang)
		}
	}
	trS, _ := buildEmail(a, "tr")
	enS, _ := buildEmail(a, "en")
	if trS == enS {
		t.Error("tr and en e-mails are identical")
	}
}
