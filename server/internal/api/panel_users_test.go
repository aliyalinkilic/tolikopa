package api

import (
	"strings"
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
)

func TestGuardAdminChange(t *testing.T) {
	cases := []struct {
		name string
		ch   adminChange
		want string
	}{
		{"demote another admin, one admin remains",
			adminChange{TargetIsEnabledAdmin: true, RemovesAdmin: true, OtherEnabledAdmins: 1}, ""},
		{"demote the only enabled admin",
			adminChange{TargetIsEnabledAdmin: true, RemovesAdmin: true, OtherEnabledAdmins: 0}, apierr.LastAdmin},
		{"delete the only enabled admin",
			adminChange{TargetIsEnabledAdmin: true, RemovesAdmin: true, SelfForbidden: true, OtherEnabledAdmins: 0}, apierr.LastAdmin},
		{"delete a viewer when no other admin exists",
			adminChange{TargetIsEnabledAdmin: false, RemovesAdmin: true, OtherEnabledAdmins: 0}, ""},
		{"delete an already-disabled admin",
			adminChange{TargetIsEnabledAdmin: false, RemovesAdmin: true, OtherEnabledAdmins: 0}, ""},
		{"rename the only admin (no admin removed)",
			adminChange{Self: true, TargetIsEnabledAdmin: true, OtherEnabledAdmins: 0}, ""},
		{"self demote with other admins present",
			adminChange{Self: true, SelfForbidden: true, TargetIsEnabledAdmin: true, RemovesAdmin: true, OtherEnabledAdmins: 3}, apierr.CannotModifySelf},
		{"self delete as the last admin reports self, not last_admin",
			adminChange{Self: true, SelfForbidden: true, TargetIsEnabledAdmin: true, RemovesAdmin: true, OtherEnabledAdmins: 0}, apierr.CannotModifySelf},
		{"self password/2fa reset through the admin endpoint",
			adminChange{Self: true, SelfForbidden: true}, apierr.CannotModifySelf},
		{"reset another user's password",
			adminChange{Self: false, SelfForbidden: true}, ""},
	}
	for _, tc := range cases {
		if got := guardAdminChange(tc.ch); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	for _, code := range []string{apierr.LastAdmin, apierr.CannotModifySelf} {
		status, body := apierr.Resolve(guardError(code))
		if status != 409 || body.Code != code {
			t.Errorf("guardError(%s) = %d %q", code, status, body.Code)
		}
	}
}

func TestValidUsername(t *testing.T) {
	for _, ok := range []string{"admin", "ayse.yilmaz", "it-ops_2", "ayşe", "a@corp.example.com", "x"} {
		if !validUsername(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a/b", "a:b", "a\nb", "a%2Fb", `CORP\ayse`, strings.Repeat("a", 65)} {
		if validUsername(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !validRole("admin") || !validRole("viewer") || validRole("root") || validRole("") {
		t.Error("validRole")
	}
}
