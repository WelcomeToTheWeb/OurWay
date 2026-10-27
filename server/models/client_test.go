package models

import "testing"

// The slug is the client's URL identity (`?client=<slug>`, 2.3-5) and the
// tenancy's join key, so its normalization rule is load-bearing: two clients
// that normalize to the same slug are the same client, and a slug the system
// invented for an un-nameable input is a client nobody asked for.
func TestNormalizeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Acme Widgets", "acme-widgets"},
		{"  ACME  Widgets  ", "acme-widgets"},
		{"acme_widgets", "acme-widgets"},
		{"Acme-Co.", "acme-co."},
		{"acme:eu", "acme:eu"},
		{"Acme Ltd 2", "acme-ltd-2"},
		// Characters tags already forbid are dropped, not escaped. The slash is
		// removed, so "Acme/Widg" and "AcmeWidg" normalize to the same slug —
		// which is the collision rule catching them, not the slug inventing one.
		{"Acme/Widg!", "acmewidg"},
		// Nothing legal in it → empty, and the caller reports it.
		{"!!!", ""},
		{"   ", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeSlug(c.in); got != c.want {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// The 32-char ceiling tags already use, applied after normalization.
	long := NormalizeSlug("a-very-long-client-name-that-keeps-going-and-going-and-going")
	if len(long) > 32 {
		t.Errorf("normalized slug is %d chars (%q), want at most 32", len(long), long)
	}

	// Normalization is idempotent: the console re-submits a slug it received.
	if twice := NormalizeSlug(long); twice != long {
		t.Errorf("normalizing a normalized slug changed it: %q → %q", long, twice)
	}
}

// The unassigned sentinel is a name for a NULL. These helpers are the only
// places that decide "is this the unassigned bucket", so the definition has one
// owner (2.3-0's rule).
func TestUnassignedSentinelIsCaseInsensitive(t *testing.T) {
	for _, v := range []string{"unassigned", "Unassigned", " UNASSIGNED "} {
		if !IsUnassignedClientID(v) {
			t.Errorf("IsUnassignedClientID(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"acme", "unassigned-co", ""} {
		if IsUnassignedClientID(v) {
			t.Errorf("IsUnassignedClientID(%q) = true, want false", v)
		}
	}
}
