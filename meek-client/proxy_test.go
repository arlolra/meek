package main

import (
	"os"
	"testing"
)

func TestGetProxyURL(t *testing.T) {
	badTests := [...]string{
		"bogus",
		"http:",
		"://127.0.0.1",
		"//127.0.0.1",
		"http:127.0.0.1",
		"://[::1]",
		"//[::1]",
		"http:[::1]",
		"://localhost",
		"//localhost",
		"http:localhost",
	}
	goodTests := [...]struct {
		input, expected string
	}{
		{"http://127.0.0.1", "http://127.0.0.1"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080/"},
		{"http://127.0.0.1:8080/path", "http://127.0.0.1:8080/path"},
		{"http://[::1]", "http://[::1]"},
		{"http://[::1]:8080", "http://[::1]:8080"},
		{"http://[::1]:8080/", "http://[::1]:8080/"},
		{"http://[::1]:8080/path", "http://[::1]:8080/path"},
		{"http://localhost", "http://localhost"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"http://localhost:8080/", "http://localhost:8080/"},
		{"http://localhost:8080/path", "http://localhost:8080/path"},
		{"http://user@localhost:8080", "http://user@localhost:8080"},
		{"http://user:password@localhost:8080", "http://user:password@localhost:8080"},
		{"socks5://localhost:1080", "socks5://localhost:1080"},
		{"socks4a://localhost:1080", "socks4a://localhost:1080"},
		{"unknown://localhost/whatever", "unknown://localhost/whatever"},
	}
	/*
		No port: reject; or infer from scheme?
			http://localhost
			socks4a://localhost
			socks5://localhost
		Port without host: probably reject?
			http://:8080
			socks4a://:1080
			socks5://:1080
	*/

	os.Clearenv()
	u, err := PtGetProxyURL()
	if err != nil {
		t.Errorf("empty environment unexpectedly returned an error: %s", err)
	}
	if u != nil {
		t.Errorf("empty environment returned %q", u)
	}

	for _, input := range badTests {
		os.Setenv("TOR_PT_PROXY", input)
		u, err = PtGetProxyURL()
		if err == nil {
			t.Errorf("TOR_PT_PROXY=%q unexpectedly succeeded and returned %q", input, u)
		}
	}

	for _, test := range goodTests {
		os.Setenv("TOR_PT_PROXY", test.input)
		u, err := PtGetProxyURL()
		if err != nil {
			t.Errorf("TOR_PT_PROXY=%q unexpectedly returned an error: %s", test.input, err)
		}
		if u == nil || u.String() != test.expected {
			t.Errorf("TOR_PT_PROXY=%q → %q (expected %q)", test.input, u, test.expected)
		}
	}
}
