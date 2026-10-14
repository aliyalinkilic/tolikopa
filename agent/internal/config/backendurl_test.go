package config

import "testing"

func TestValidateBackendURL(t *testing.T) {
	ok := []string{
		"https://tolikopa.corp.example.com",
		"https://10.0.0.10",
		"https://10.0.0.10:8443/",
		"HTTPS://tolikopa.corp.example.com", // url.Parse lowercases the scheme
		"  https://tolikopa.local  ",
	}
	for _, c := range ok {
		if err := ValidateBackendURL(c); err != nil {
			t.Errorf("%q: unexpected error %v", c, err)
		}
	}
	bad := []string{
		"http://tolikopa.corp.example.com",
		"HTTP://10.0.0.10",
		"tolikopa.corp.example.com",
		"//tolikopa.corp.example.com",
		"ftp://10.0.0.10",
		"https://",
		"https:///path",
		"https://user:pass@10.0.0.10",
		"",
		"://bad",
	}
	for _, c := range bad {
		if err := ValidateBackendURL(c); err == nil {
			t.Errorf("%q: accepted", c)
		}
	}
	if err := ValidateBackendURL(Default().BackendURL); err != nil {
		t.Errorf("default backend_url rejected: %v", err)
	}
}
