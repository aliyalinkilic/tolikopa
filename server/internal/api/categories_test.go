package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

func TestSortUnclassified(t *testing.T) {
	list := []model.UnclassifiedProcess{
		{Name: "b", Count: 10, Machines: 3, CPUShare: 0.01},
		{Name: "a", Count: 99, Machines: 1, CPUShare: 0.50},
		{Name: "c", Count: 5, Machines: 3, CPUShare: 0.20},
		{Name: "d", Count: 5, Machines: 3, CPUShare: 0.20},
	}
	order := func() string {
		var b strings.Builder
		for _, u := range list {
			b.WriteString(u.Name)
		}
		return b.String()
	}
	sortUnclassified(list, "machines")
	if got := order(); got != "cdba" { // machines, then cpu, then count, then name
		t.Errorf("machines order = %s", got)
	}
	sortUnclassified(list, "cpu")
	if got := order(); got != "acdb" {
		t.Errorf("cpu order = %s", got)
	}
	sortUnclassified(list, "count")
	if got := order(); got != "abcd" {
		t.Errorf("count order = %s", got)
	}
}

func TestReclassifyNamesValidation(t *testing.T) {
	a := &API{}
	ctx := context.Background()
	now := time.Now()

	got, err := a.reclassifyNames(ctx, []string{"Chrome.EXE", "chrome", `C:\x\Tool.exe`, "  "}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "chrome,tool" {
		t.Errorf("names = %v, want normalised and de-duplicated", got)
	}

	_, err = a.reclassifyNames(ctx, []string{}, now, now)
	wantInvalid(t, err, "names")

	many := make([]string, maxReclassifyNames+1)
	for i := range many {
		many[i] = "x"
	}
	_, err = a.reclassifyNames(ctx, many, now, now)
	wantInvalid(t, err, "names")

	_, err = a.reclassifyNames(ctx, []string{strings.Repeat("a", maxMatchValueLen+1)}, now, now)
	wantInvalid(t, err, "names")
}

func TestTestCategoryValidation(t *testing.T) {
	a := &API{}
	for _, q := range []string{"", "name=", "name=%20", "name=" + strings.Repeat("a", maxMatchValueLen+1)} {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/categories/test?"+q, nil)
		c := echo.New().NewContext(req, httptest.NewRecorder())
		wantInvalid(t, a.TestCategory(c), "name")
	}
}

func TestUnclassifiedRejectsUnknownSort(t *testing.T) {
	a := &API{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/unclassified?sort=name", nil)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	wantInvalid(t, a.Unclassified(c), "sort")
}
