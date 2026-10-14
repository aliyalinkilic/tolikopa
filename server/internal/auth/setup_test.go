package auth

import (
	"regexp"
	"strings"
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/config"
)

func TestSetupCodeFormat(t *testing.T) {
	re := regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}(-[A-HJ-NP-Z2-9]{4}){3}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := newSetupCode()
		if !re.MatchString(c) {
			t.Fatalf("code %q is not 4x4 from the unambiguous alphabet", c)
		}
		if seen[c] {
			t.Fatalf("code %q generated twice", c)
		}
		seen[c] = true
	}
	if len(setupAlphabet) != 32 || strings.ContainsAny(setupAlphabet, "IO01") {
		t.Errorf("alphabet %q must be 32 unambiguous symbols", setupAlphabet)
	}
}

func TestSetupCodeCompare(t *testing.T) {
	a := testAuth()

	// Nothing armed: nothing matches, not even the empty string.
	for _, guess := range []string{"", "ABCD-EFGH-JKLM-NPQR", "0000"} {
		if a.checkSetupCode(guess) {
			t.Errorf("no code armed, yet %q was accepted", guess)
		}
	}

	code := newSetupCode()
	a.armSetupCode(code)
	plain := strings.ReplaceAll(code, "-", "")
	for _, form := range []string{code, strings.ToLower(code), plain, " " + code + "\n", strings.ReplaceAll(code, "-", " ")} {
		if !a.checkSetupCode(form) {
			t.Errorf("valid code in form %q rejected", form)
		}
	}
	for _, bad := range []string{"", plain[:15], plain + "A", code[:len(code)-1] + "!", newSetupCode(), strings.Repeat("A", 16)} {
		if a.checkSetupCode(bad) {
			t.Errorf("wrong code %q accepted", bad)
		}
	}
	// Only a hash is kept.
	if a.setup.codeHash == nil || strings.Contains(string(a.setup.codeHash[:]), plain) {
		t.Error("the setup code is held in clear")
	}

	// The code dies when the admin is created.
	a.armSetupCode("")
	if a.checkSetupCode(code) {
		t.Error("code still accepted after it was disarmed")
	}
}

// An operator-supplied SETUP_CODE is compared the same way.
func TestSetupCodeFromEnvForm(t *testing.T) {
	a := New(nil, &config.Config{JWTSecret: "x", SetupCode: "my-Install-Code-42"})
	a.armSetupCode(a.cfg.SetupCode)
	if !a.checkSetupCode("MY-INSTALL-CODE-42") || !a.checkSetupCode("myinstallcode42") {
		t.Error("operator code not accepted case/dash-insensitively")
	}
	if a.checkSetupCode("my-install-code-43") {
		t.Error("wrong operator code accepted")
	}
}

func TestValidUsername(t *testing.T) {
	for _, ok := range []string{"admin", "ayse.yilmaz", "it-ops_2", "ayşe", "a@corp.example.com"} {
		if !ValidUsername(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a/b", "a:b", "a\nb", `CORP\ayse`, strings.Repeat("a", 65)} {
		if ValidUsername(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
