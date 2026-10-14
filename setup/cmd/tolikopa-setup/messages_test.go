package main

import (
	"os"
	"regexp"
	"testing"
)

// Every key exists in both languages, and every key the code asks for exists.
func TestMessagesComplete(t *testing.T) {
	for k := range messages["en"] {
		if _, ok := messages["tr"][k]; !ok {
			t.Errorf("tr is missing %q", k)
		}
	}
	for k := range messages["tr"] {
		if _, ok := messages["en"][k]; !ok {
			t.Errorf("en is missing %q", k)
		}
	}
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`\bt\("([a-z_]+)"`).FindAllSubmatch(src, -1) {
		if _, ok := messages["en"][string(m[1])]; !ok {
			t.Errorf("main.go uses unknown message %q", m[1])
		}
	}
}
