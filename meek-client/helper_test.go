package main

import (
	"net/url"
	"testing"
)

func TestMakeProxySpec(t *testing.T) {
	badTests := [...]url.URL{
		url.URL{Scheme: "http"},
		url.URL{Scheme: "http", Host: ":"},
		url.URL{Scheme: "http", Host: "localhost"},
		url.URL{Scheme: "http", Host: "localhost:"},
		url.URL{Scheme: "http", Host: ":8080"},
		url.URL{Scheme: "http", Host: "localhost:https"},
		url.URL{Scheme: "http", Host: "localhost:8080", User: url.User("username")},
		url.URL{Scheme: "http", Host: "localhost:8080", User: url.UserPassword("username", "password")},
		url.URL{Scheme: "http", User: url.User("username"), Host: "localhost:8080"},
		url.URL{Scheme: "http", User: url.UserPassword("username", "password"), Host: "localhost:8080"},
		url.URL{Scheme: "http", Host: "localhost:-1"},
		url.URL{Scheme: "http", Host: "localhost:65536"},
		url.URL{Scheme: "unknown", Host: "localhost:9999"},
	}
	goodTests := [...]struct {
		input    url.URL
		expected ProxySpec
	}{
		{
			url.URL{Scheme: "http", Host: "localhost:8080"},
			ProxySpec{"http", "localhost", 8080},
		},
	}

	for _, input := range badTests {
		_, err := makeProxySpec(&input)
		if err == nil {
			t.Errorf("%q unexpectedly succeeded", input)
		}
	}

	for _, test := range goodTests {
		spec, err := makeProxySpec(&test.input)
		if err != nil {
			t.Fatalf("%q unexpectedly returned an error: %s", test.input, err)
		}
		if *spec != test.expected {
			t.Errorf("%q → %q (expected %q)", test.input, spec, test.expected)
		}
	}
}
