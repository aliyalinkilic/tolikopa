package service

import (
	"fmt"
	"strings"
	"testing"
)

type recFocus map[string]int

func (r recFocus) AddAppFocus(name string, sec int) { r[name] += sec }

type recIdle struct{ last, calls int }

func (r *recIdle) ReportIdle(sec int) { r.last, r.calls = sec, r.calls+1 }

func TestReadHelperLinesHostileInput(t *testing.T) {
	var b strings.Builder
	// 1) A valid line.
	b.WriteString(`{"winword":30,"__idle":5}` + "\n")
	// 2) An oversize line (one huge name) must be discarded whole...
	b.WriteString(`{"` + strings.Repeat("x", 3*fgMaxLine) + `":30}` + "\n")
	// 3) ...without swallowing the next valid line.
	b.WriteString(`{"excel":10}` + "\n")
	// 4) Garbage.
	b.WriteString("not json\n")
	// 5) Too many names, invalid names, absurd seconds.
	var many []string
	for i := 0; i < 500; i++ {
		many = append(many, fmt.Sprintf(`"app%d":1`, i))
	}
	b.WriteString("{" + strings.Join(many, ",") + "}\n")
	b.WriteString(`{"C:\\Users\\x\\secret.exe":30,"a/b":30,"` + strings.Repeat("a", 200) + `":30,"outlook":999999,"__evil":1}` + "\n")
	// 6) Unterminated trailing line: dropped.
	b.WriteString(`{"teams":30}`)

	focus := recFocus{}
	idle := &recIdle{}
	readHelperLines(strings.NewReader(b.String()), focus, idle)

	if focus["winword"] != 30 || focus["excel"] != 10 {
		t.Fatalf("valid lines not applied: %v", focus)
	}
	if focus["outlook"] != fgMaxSecPerLine {
		t.Fatalf("seconds not clamped: %d", focus["outlook"])
	}
	if _, ok := focus["teams"]; ok {
		t.Fatal("unterminated line applied")
	}
	for name := range focus {
		if strings.ContainsAny(name, `/\`) || len(name) > 128 || strings.HasPrefix(name, "__") || strings.Contains(name, "xxxx") {
			t.Fatalf("invalid name accepted: %q", name)
		}
	}
	apps := 0
	for name := range focus {
		if strings.HasPrefix(name, "app") {
			apps++
		}
	}
	if apps > fgMaxEntriesPerLine {
		t.Fatalf("took %d names from one line, cap %d", apps, fgMaxEntriesPerLine)
	}
	if idle.calls != 1 || idle.last != 5 {
		t.Fatalf("idle = %+v", idle)
	}
}
