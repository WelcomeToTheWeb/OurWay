package main

import "testing"

func TestParseLaunchURL(t *testing.T) {
	l, err := ParseLaunchURL("ourway://session/abc-123?server=https%3A%2F%2Frmm.example.com%2F&token=tok&device=PC%201")
	if err != nil {
		t.Fatal(err)
	}
	if l.SessionID != "abc-123" || l.Server != "https://rmm.example.com" || l.Token != "tok" || l.Device != "PC 1" {
		t.Errorf("parsed wrong: %+v", l)
	}
	for _, bad := range []string{
		"https://session/abc?server=https://x&token=t",
		"ourway://other/abc?server=https://x&token=t",
		"ourway://session/?server=https://x&token=t",
		"ourway://session/abc?server=https://x",
		"ourway://session/abc?server=file:///etc&token=t",
		"ourway://session/abc?server=javascript:1&token=t",
		"ourway://session/a/b?server=https://x&token=t",
	} {
		if _, err := ParseLaunchURL(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
