package admincli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type fakeStore struct {
	users       map[string]bool
	reset2FA    []string
	passwordFor string
	hash        string
	policyOff   bool
}

func (f *fakeStore) Reset2FA(_ context.Context, u string) (bool, error) {
	if !f.users[u] {
		return false, nil
	}
	f.reset2FA = append(f.reset2FA, u)
	return true, nil
}

func (f *fakeStore) SetPassword(_ context.Context, u, hash string) (bool, error) {
	if !f.users[u] {
		return false, nil
	}
	f.passwordFor, f.hash = u, hash
	return true, nil
}

func (f *fakeStore) Require2FAOff(context.Context) error { f.policyOff = true; return nil }

func (f *fakeStore) CreateFirstAdmin(_ context.Context, u, hash string) (bool, error) {
	if len(f.users) > 0 {
		return false, nil
	}
	f.users = map[string]bool{u: true}
	f.passwordFor, f.hash = u, hash
	return true, nil
}

func exec(st store, stdin string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), st, args, strings.NewReader(stdin), &out, &errb, false)
	return code, out.String(), errb.String()
}

func TestReset2FA(t *testing.T) {
	st := &fakeStore{users: map[string]bool{"admin": true}}
	code, out, _ := exec(st, "", "reset-2fa", "admin")
	if code != 0 || len(st.reset2FA) != 1 || strings.Count(out, "\n") != 1 {
		t.Fatalf("code=%d out=%q calls=%v", code, out, st.reset2FA)
	}
	if code, _, errOut := exec(st, "", "reset-2fa", "ghost"); code != 1 || !strings.Contains(errOut, "no panel user") {
		t.Errorf("unknown user: code=%d err=%q", code, errOut)
	}
}

func TestResetPasswordReadsStdinAndNeverEchoesIt(t *testing.T) {
	st := &fakeStore{users: map[string]bool{"admin": true}}
	const pw = "correct horse battery"
	code, out, errOut := exec(st, pw+"\r\n", "reset-password", "admin")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if bcrypt.CompareHashAndPassword([]byte(st.hash), []byte(pw)) != nil {
		t.Error("stored hash does not match the password read from stdin")
	}
	if strings.Contains(out+errOut, pw) {
		t.Error("the password was written to the output")
	}
}

func TestResetPasswordEnforcesPolicy(t *testing.T) {
	st := &fakeStore{users: map[string]bool{"admin": true}}
	if code, _, _ := exec(st, "short\n", "reset-password", "admin"); code != 1 || st.hash != "" {
		t.Errorf("weak password accepted: code=%d", code)
	}
	if code, _, _ := exec(st, "", "reset-password", "admin"); code != 1 || st.hash != "" {
		t.Errorf("empty stdin accepted: code=%d", code)
	}
}

// The password must never be an argument; an extra argv word is a usage error.
func TestPasswordNotAcceptedFromArgv(t *testing.T) {
	st := &fakeStore{users: map[string]bool{"admin": true}}
	if code, _, _ := exec(st, "", "reset-password", "admin", "hunter2hunter2"); code != 2 || st.hash != "" {
		t.Errorf("argv password was not rejected: code=%d", code)
	}
}

func TestRequire2FA(t *testing.T) {
	st := &fakeStore{}
	if code, _, _ := exec(st, "", "require-2fa", "off"); code != 0 || !st.policyOff {
		t.Errorf("off: code=%d policyOff=%v", code, st.policyOff)
	}
	st = &fakeStore{}
	if code, _, _ := exec(st, "", "require-2fa", "on"); code != 2 || st.policyOff {
		t.Errorf("on must be refused: code=%d", code)
	}
}

func TestInitAdminOnlyOnEmptyTable(t *testing.T) {
	st := &fakeStore{}
	const pw = "a long enough password"
	code, out, errOut := exec(st, pw+"\n", "init-admin", "ayse")
	if code != 0 || st.passwordFor != "ayse" || strings.Contains(out+errOut, pw) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if bcrypt.CompareHashAndPassword([]byte(st.hash), []byte(pw)) != nil {
		t.Fatal("stored hash does not match the password")
	}
	if code, _, errOut := exec(st, pw+"\n", "init-admin", "mehmet"); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Errorf("second admin: code=%d err=%q", code, errOut)
	}
	if code, _, _ := exec(&fakeStore{}, "short\n", "init-admin", "ayse"); code != 1 {
		t.Error("weak password accepted")
	}
	if code, _, _ := exec(&fakeStore{}, pw+"\n", "init-admin", "bad name"); code != 2 {
		t.Error("invalid username accepted")
	}
}
