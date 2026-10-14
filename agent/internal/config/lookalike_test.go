package config

import "testing"

func TestRejectLookalikeKeys(t *testing.T) {
	ok := []string{
		`{"backend_url":"https://x","allow_app_removal":true,"enforce_actions":false}`,
		`{"backend_url":"https://x"}`,
		`not json`,
	}
	for _, c := range ok {
		if err := rejectLookalikeKeys([]byte(c)); err != nil {
			t.Errorf("%s: unexpected error %v", c, err)
		}
	}
	bad := []string{
		`{"ALLOW_APP_REMOVAL":true}`,
		`{"Enforce_Actions":true}`,
		"{\"allow_ſtartup_control\":true}",
		"{\"enforce_actionſ\":true}",
	}
	for _, c := range bad {
		if err := rejectLookalikeKeys([]byte(c)); err == nil {
			t.Errorf("%s: accepted", c)
		}
	}
}
