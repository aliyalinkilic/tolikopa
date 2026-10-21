package api

import (
	"fmt"
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

func TestTopUnclassifiedNames(t *testing.T) {
	byName := map[string]*model.UnclassifiedProcess{}
	cpu := map[string]float64{}
	for i := 0; i < 10; i++ {
		n := fmt.Sprintf("p%d", i)
		byName[n] = &model.UnclassifiedProcess{Name: n, Count: int64(i)}
		cpu[n] = float64(10 - i)
	}

	got := topUnclassifiedNames(byName, cpu, "count", 3)
	for _, n := range []string{"p9", "p8", "p7"} {
		if _, ok := got[n]; !ok || len(got) != 3 {
			t.Fatalf("count: got %v, want p9 p8 p7", got)
		}
	}
	got = topUnclassifiedNames(byName, cpu, "cpu", 2)
	for _, n := range []string{"p0", "p1"} {
		if _, ok := got[n]; !ok || len(got) != 2 {
			t.Fatalf("cpu: got %v, want p0 p1", got)
		}
	}
	if got := topUnclassifiedNames(byName, cpu, "machines", 100); len(got) != 10 {
		t.Fatalf("under the cap every name is kept, got %d", len(got))
	}
}
