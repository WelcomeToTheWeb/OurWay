package sso

import "testing"

// The contract's two predicates, tested where they live. The domain allow-list is a
// wall an operator draws around their identity provider, and a wall with a hole in
// the comparison — a case-insensitive domain, a local part containing an `@`, a
// trailing space — is a wall reported as drawn.

func TestDomainAllowed(t *testing.T) {
	cases := []struct {
		name    string
		list    string
		email   string
		allowed bool
	}{
		{"no list is no restriction", "", "anyone@anywhere.example", true},
		{"empty list is no restriction", "   ", "anyone@anywhere.example", true},
		{"listed domain", "corp.example", "ops@corp.example", true},
		{"case-insensitive on both sides", "CORP.Example", "OPS@Corp.EXAMPLE", true},
		{"surrounding spaces in the list", " corp.example , partner.example ", "ops@corp.example", true},
		{"a domain that only shares a suffix", "corp.example", "ops@notcorp.example", false},
		{"a subdomain is not the domain", "corp.example", "ops@idp.corp.example", false},
		{"the local part is not the domain", "corp.example", "corp.example@evil.example", false},
		{"no at sign is not in the list", "corp.example", "ops", false},
		{"an empty domain is not in the list", "corp.example", "ops@", false},
		{"empty email is refused", "corp.example", "", false},
		{"multiple entries", "corp.example,partner.example", "ops@partner.example", true},
		{"empty entries do not open the wall", "corp.example,,", "ops@evil.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.email == "" {
				// The empty assertion is the API's `no_email_claim` refusal, not a
				// domain comparison: `contractGate` refuses it before this is reached.
				return
			}
			if got := domainAllowed(tc.list, tc.email); got != tc.allowed {
				t.Errorf("domainAllowed(%q, %q) = %v, want %v", tc.list, tc.email, got, tc.allowed)
			}
		})
	}
}

func TestParseRoleMapping(t *testing.T) {
	if got := parseRoleMapping(""); len(got) != 0 {
		t.Errorf("an empty mapping = %v, want no mapping (and the viewer floor)", got)
	}
	if got := parseRoleMapping("   "); len(got) != 0 {
		t.Errorf("a whitespace mapping = %v, want no mapping", got)
	}
	if got := parseRoleMapping("not json"); len(got) != 0 {
		t.Errorf("unparseable mapping = %v, want no mapping: a stale row must not grant a role", got)
	}
	if got := parseRoleMapping(`["admin"]`); len(got) != 0 {
		t.Errorf("an array mapping = %v, want no mapping", got)
	}
	got := parseRoleMapping(`{"rmm-admins":"admin"," rmm-tech ":" technician ","":"admin"}`)
	if got["rmm-admins"] != "admin" || got["rmm-tech"] != "technician" {
		t.Errorf("mapping = %v, want the claim values and roles trimmed and mapped", got)
	}
	if len(got) != 2 {
		t.Errorf("mapping has %d entries, want 2: an empty claim value is not a rule", len(got))
	}
	if len(parseRoleMapping(`{"group":""}`)) != 0 {
		t.Error("a mapping naming no role is a mapping")
	}
}

func TestMappingOwnsRoles(t *testing.T) {
	owned := mappingOwnsRoles(map[string]string{"a": "admin", "b": "technician", "c": "admin"})
	if len(owned) != 2 || !owned["admin"] || !owned["technician"] {
		t.Errorf("owned roles = %v, want the mapping's role names", owned)
	}
	if len(mappingOwnsRoles(nil)) != 0 {
		t.Error("no mapping owns no roles")
	}
}
