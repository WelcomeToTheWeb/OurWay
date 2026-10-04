package main

import (
	"errors"
	"net/url"
	"strings"
)

// Launch is what the web console passes in the ourway:// URL:
//
//	ourway://session/<id>?server=<https://host>&token=<viewer token>&device=<name>
type Launch struct {
	SessionID string
	Server    string
	Token     string
	Device    string
}

// ParseLaunchURL validates and decodes a launch URL. The URL arrives
// from a browser (any web page can trigger an ourway:// link), so it
// is treated as untrusted: only http(s) servers are accepted.
func ParseLaunchURL(raw string) (Launch, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Launch{}, err
	}
	if u.Scheme != "ourway" || u.Host != "session" {
		return Launch{}, errors.New("not an ourway://session/ URL")
	}
	id := strings.Trim(u.Path, "/")
	q := u.Query()
	l := Launch{SessionID: id, Server: strings.TrimRight(q.Get("server"), "/"), Token: q.Get("token"), Device: q.Get("device")}
	if l.SessionID == "" || strings.Contains(l.SessionID, "/") {
		return Launch{}, errors.New("missing session id")
	}
	if l.Token == "" {
		return Launch{}, errors.New("missing token")
	}
	su, err := url.Parse(l.Server)
	if err != nil || (su.Scheme != "http" && su.Scheme != "https") || su.Host == "" {
		return Launch{}, errors.New("server must be an http(s) URL")
	}
	return l, nil
}
