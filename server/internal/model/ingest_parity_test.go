package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The agent and the server do not import each other, so the ingest wire contract
// is written twice (agent/internal/model/model.go and ingest.go here). This test
// parses both files and requires every struct the agent declares to exist here
// with the same fields, Go types and json tags — a drift is a silent data loss
// (a renamed tag simply decodes as the zero value).
//
// It needs the repository layout; a server-only checkout skips it.

// serverOnlyFields are deliberate, documented server-side additions.
var serverOnlyFields = map[string]bool{
	// docs/API.md §1: kept for old agents; the agent never reads it.
	"IngestResponse.Rejected": true,
}

type fieldSig struct{ typ, tag string }

func structFields(t *testing.T, path string) map[string]map[string]fieldSig {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]map[string]fieldSig{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		fields := map[string]fieldSig{}
		for _, fl := range st.Fields.List {
			var b strings.Builder
			_ = writeExpr(&b, fl.Type)
			tag := ""
			if fl.Tag != nil {
				tag = reflect.StructTag(strings.Trim(fl.Tag.Value, "`")).Get("json")
			}
			for _, name := range fl.Names {
				fields[name.Name] = fieldSig{typ: b.String(), tag: tag}
			}
		}
		out[ts.Name.Name] = fields
		return true
	})
	return out
}

func writeExpr(b *strings.Builder, e ast.Expr) error {
	switch x := e.(type) {
	case *ast.Ident:
		b.WriteString(x.Name)
	case *ast.StarExpr:
		b.WriteString("*")
		return writeExpr(b, x.X)
	case *ast.ArrayType:
		b.WriteString("[]")
		return writeExpr(b, x.Elt)
	case *ast.MapType:
		b.WriteString("map[")
		_ = writeExpr(b, x.Key)
		b.WriteString("]")
		return writeExpr(b, x.Value)
	case *ast.SelectorExpr:
		_ = writeExpr(b, x.X)
		b.WriteString("." + x.Sel.Name)
	default:
		b.WriteString("?")
	}
	return nil
}

func TestIngestContractMatchesAgent(t *testing.T) {
	agentPath := filepath.Join("..", "..", "..", "agent", "internal", "model", "model.go")
	if _, err := os.Stat(agentPath); err != nil {
		t.Skipf("agent module not present (%v)", err)
	}
	agent := structFields(t, agentPath)
	server := structFields(t, "ingest.go")
	if len(agent) == 0 {
		t.Fatal("no structs found in the agent model")
	}
	for name, af := range agent {
		sf, ok := server[name]
		if !ok {
			t.Errorf("struct %s exists in the agent model but not in server/internal/model/ingest.go", name)
			continue
		}
		for field, a := range af {
			s, ok := sf[field]
			if !ok {
				t.Errorf("%s.%s (json %q) is sent by the agent but missing on the server", name, field, a.tag)
				continue
			}
			if a != s {
				t.Errorf("%s.%s: agent %s `json:%q`, server %s `json:%q`", name, field, a.typ, a.tag, s.typ, s.tag)
			}
		}
		for field := range sf {
			if _, ok := af[field]; !ok && !serverOnlyFields[name+"."+field] {
				t.Errorf("%s.%s exists only on the server side", name, field)
			}
		}
	}
}
