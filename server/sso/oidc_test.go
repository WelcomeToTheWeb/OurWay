package sso

import (
	"testing"

	"ourway/server/sso/testidp"
)

func discoveryTarget(p *testidp.IdP) (*DiscoveryDocument, error) {
	return Discover(DefaultHTTPClient(), p.Issuer(), "")
}

// Discovery (2.4-1): the issuer is the field an operator types, and everything
// the flow calls comes from the provider's own document.
func TestDiscoveryAcceptsAConformantProvider(t *testing.T) {
	p := testidp.New()
	defer p.Close()

	doc, err := discoveryTarget(p)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if doc.Issuer != p.Issuer() {
		t.Errorf("issuer = %q, want %q", doc.Issuer, p.Issuer())
	}
	for name, got := range map[string]string{
		"authorization_endpoint": doc.AuthorizationEndpoint,
		"token_endpoint":         doc.TokenEndpoint,
		"userinfo_endpoint":      doc.UserInfoEndpoint,
		"jwks_uri":               doc.JwksURI,
	} {
		if got == "" {
			t.Errorf("%s is empty in a conformant document", name)
		}
	}
	if !doc.PKCESupported() {
		t.Error("the provider publishes S256; PKCESupported() says it does not")
	}
	if !doc.ScopeSupports("email") {
		t.Error("the provider lists the email scope; ScopeSupports() says it does not")
	}
	if doc.Raw == "" {
		t.Error("the document is not kept verbatim, so the console cannot show what was discovered")
	}
}

// Each failure has its own name, because the console has to answer with a state an
// operator can act on (2.4-4), and a provider saved from a broken document is a
// login that fails with a 500 at 2 a.m.
func TestDiscoveryRefusesEveryBrokenShape(t *testing.T) {
	cases := []struct {
		name   string
		broken string
		kind   string
	}{
		{"the document is not there", "off", DiscoveryUnreachable},
		{"the document is not JSON", "garbage", DiscoveryBadDocument},
		{"no jwks_uri", "no_jwks", DiscoveryBadDocument},
		{"the document describes another issuer", "issuer", DiscoveryIssuerMismatch},
		{"no authorization-code flow", "no_code", DiscoveryNoCodeFlow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testidp.New()
			defer p.Close()
			p.BreakDocument(tc.broken)

			_, err := discoveryTarget(p)
			if err == nil {
				t.Fatalf("a broken provider was accepted")
			}
			de, ok := err.(*DiscoveryError)
			if !ok {
				t.Fatalf("error is not a DiscoveryError: %T %v", err, err)
			}
			if de.Kind != tc.kind {
				t.Errorf("kind = %q, want %q (%v)", de.Kind, tc.kind, de.Err)
			}
		})
	}
}

func TestDiscoveryTrailingSlashOnTheIssuerIsTheSameIssuer(t *testing.T) {
	// The issuer is typed by hand into a form. "https://idp.corp/" and
	// "https://idp.corp" name one provider, and a check that treats them as two
	// refuses the configuration at save time for no reason.
	p := testidp.New()
	defer p.Close()

	if _, err := Discover(DefaultHTTPClient(), p.Issuer()+"/", ""); err != nil {
		t.Fatalf("issuer with a trailing slash refused: %v", err)
	}
}

// Verification (2.4-2): the identity a login rests on.
func TestVerifyIDTokenAcceptsWhatTheProviderSigned(t *testing.T) {
	p := testidp.New()
	defer p.Close()
	doc, err := discoveryTarget(p)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	claims := testidp.Claims{Subject: "user-1", Email: "tech@corp.example", EmailVerified: true, Name: "Tech"}
	token := p.MintIDToken(claims, "ourway-client", "RS256")

	got, err := NewJWKSClient(nil).VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got["sub"] != "user-1" {
		t.Errorf("sub = %v, want user-1", got["sub"])
	}
	if got["email"] != "tech@corp.example" {
		t.Errorf("email = %v", got["email"])
	}
}

// The refusals, each with its own cause. A verifier that refuses them all with the
// same opaque string is a verifier an operator cannot debug.
func TestVerifyIDTokenRefusesTheCasesThatMatter(t *testing.T) {
	p := testidp.New()
	defer p.Close()
	doc, err := discoveryTarget(p)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	client := NewJWKSClient(nil)
	claims := testidp.Claims{Subject: "user-2", Email: "ops@corp.example", EmailVerified: true}

	t.Run("signed by a key the provider does not publish", func(t *testing.T) {
		rogue := p.MintIDTokenWithKey(claims, "ourway-client")
		if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", rogue); err == nil {
			t.Fatal("a token signed by an unpublished key was accepted")
		}
	})

	t.Run("minted for a different client", func(t *testing.T) {
		foreign := p.MintIDToken(claims, "someone-elses-app", "RS256")
		_, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", foreign)
		if err == nil {
			t.Fatal("a token whose aud is not our client id was accepted")
		}
	})

	t.Run("issued by another issuer", func(t *testing.T) {
		token := p.MintIDToken(claims, "ourway-client", "RS256")
		if _, err := client.VerifyIDToken(doc.JwksURI, "https://not-the-configured-issuer", "ourway-client", token); err == nil {
			t.Fatal("an iss mismatch was accepted")
		}
	})

	t.Run("expired", func(t *testing.T) {
		expired := p.MintExpiredIDToken(claims, "ourway-client")
		if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", expired); err == nil {
			t.Fatal("an expired token was accepted")
		}
	})

	t.Run("alg none", func(t *testing.T) {
		none := p.MintIDToken(claims, "ourway-client", "none")
		_, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", none)
		if err == nil {
			t.Fatal("an unsigned token was accepted")
		}
	})

	t.Run("alg HS256 against an RSA key set", func(t *testing.T) {
		hs := p.MintIDToken(claims, "ourway-client", "HS256")
		if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", hs); err == nil {
			t.Fatal("an HS256 token was verified against the provider's RSA keys")
		}
	})

	t.Run("not a JWS at all", func(t *testing.T) {
		if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", "nonsense"); err == nil {
			t.Fatal("a string that is not a JWS was accepted")
		}
	})
}

// An IdP rotates its signing key. A deployment that refuses logins on the rotation
// is a deployment whose identity provider is a weekly outage, so: a token minted
// after the rotation verifies, on the first lookup that misses the cached set.
func TestJWKSRotationKeepsLoginsWorking(t *testing.T) {
	p := testidp.New()
	defer p.Close()
	doc, err := discoveryTarget(p)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	client := NewJWKSClient(nil)
	claims := testidp.Claims{Subject: "user-3", Email: "admin@corp.example", EmailVerified: true}

	// Warm the cache with the key set as it is today.
	if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client",
		p.MintIDToken(claims, "ourway-client", "RS256")); err != nil {
		t.Fatalf("first login: %v", err)
	}

	// The IdP rotates. The cached set does not name the new kid.
	p.Rotate()
	p.PublishOnlyCurrentKey()
	if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client",
		p.MintIDToken(claims, "ourway-client", "RS256")); err != nil {
		t.Fatalf("login after a key rotation: %v", err)
	}

	// A key the provider has stopped publishing is not a key: the refresh is not
	// an open door.
	rogue := p.MintIDTokenWithKey(claims, "ourway-client")
	if _, err := client.VerifyIDToken(doc.JwksURI, p.Issuer(), "ourway-client", rogue); err == nil {
		t.Fatal("a token signed by a key outside the published set was accepted after a refresh")
	}
}

func TestJWKProbeReportsAKeySetItCanUse(t *testing.T) {
	p := testidp.New()
	doc, err := discoveryTarget(p)
	if err != nil {
		p.Close()
		t.Fatalf("discover: %v", err)
	}
	if err := NewJWKSClient(nil).Probe(doc.JwksURI); err != nil {
		p.Close()
		t.Fatalf("probe of a published key set: %v", err)
	}

	// The provider an operator deleted from the console: the key set is gone, and
	// the test-connection probe is where they should find that out.
	uri := doc.JwksURI
	p.Close()
	if err := NewJWKSClient(nil).Probe(uri); err == nil {
		t.Fatal("probe of an unreachable key set reported success")
	}
}

func TestProbeRejectsAKeySetWithNoUsableKeys(t *testing.T) {
	p := testidp.New()
	defer p.Close()
	// A published set that contains no RSA signing key cannot verify anything.
	p.PublishOnlyCurrentKey()
	doc, err := discoveryTarget(p)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if err := NewJWKSClient(nil).Probe(doc.JwksURI); err != nil {
		t.Fatalf("expected the published RSA key to be usable: %v", err)
	}
}
