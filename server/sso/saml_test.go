package sso_test

// 2.4-7 · SAML 2.0: the assertions the SAML path makes about a document from an
// identity provider.
//
// The fixture is an implementation of the protocol (`testsaml`): it holds a signing
// key, mints assertions, and signs them with real XML-DSig. Every refusal below is
// produced by *breaking a signed document after it was signed* — tampering the
// bytes, minting with a key the provider does not pin, letting the window close —
// because a stub of the verifier would agree with whatever the verifier does, and a
// SAML SP that trusts an assertion the operator's browser POSTs has no room for
// agreement between the code and its own test.

import (
	"encoding/base64"
	"encoding/xml"
	"net/url"
	"strings"
	"testing"
	"time"

	"ourway/server/models"
	"ourway/server/sso"
	"ourway/server/sso/testsaml"
)

const (
	testACS      = "https://ourway.example/api/auth/sso/saml/keycloak/acs"
	testSPEntity = "urn:ourway:keycloak:11111111-1111-1111-1111-111111111111"
)

// samlFixture is one provider row's worth of SAML configuration: an IdP fixture, the
// certificate the operator pinned (copied from it, as an operator pastes it), and the
// SP identity and key OurWay derived for the provider.
type samlFixture struct {
	IdP   *testsaml.IdP
	Cfg   sso.SAMLConfig
	SPPEM string
}

func newSAMLFixture(t *testing.T) *samlFixture {
	t.Helper()
	idp := testsaml.New()
	certPEM, keyPEM, err := sso.GenerateSPKeyPair(testSPEntity, time.Now())
	if err != nil {
		t.Fatalf("GenerateSPKeyPair: %v", err)
	}
	provider := &models.SSOProvider{
		ID:           "11111111-1111-1111-1111-111111111111",
		Name:         "keycloak",
		Type:         models.SSOTypeSAML,
		Issuer:       "https://idp.testsaml.example/realms/ourway",
		NameIDFormat: models.SAMLNameIDFormatEmail,
	}
	cfg := sso.SAMLConfig{
		ProviderName:   provider.Name,
		ProviderID:     provider.ID,
		IdpEntityID:    idp.EntityID,
		IdpSSOURL:      idp.SSOURL,
		IdpLogoutURL:   idp.LogoutURL,
		IdpCertificate: idp.CertificatePEM(),
		SPEntityID:     sso.SPFor(provider),
		ACSURL:         testACS,
		SPCertPEM:      certPEM,
		SPKeyPEM:       keyPEM,
		SignRequests:   true,
		NameIDFormat:   models.SAMLNameIDFormatEmail,
		AttrEmail:      "email",
		AttrUsername:   "username",
		AttrName:       "displayName",
		AttrGroups:     "groups",
	}
	if cfg.SPEntityID != testSPEntity {
		t.Fatalf("SP entity id is %q, want %q: the audience the assertion must name", cfg.SPEntityID, testSPEntity)
	}
	return &samlFixture{IdP: idp, Cfg: cfg, SPPEM: certPEM}
}

// claims is the identity the IdP asserts in these tests: a person whose NameID *is*
// her email address, in the format the provider asked for, with groups the role
// mapping can read.
func samlClaims() testsaml.Claims {
	return testsaml.Claims{
		NameID:       "ada@ourway.example",
		NameIDFormat: testsaml.NameIDEmail,
		Email:        "ada@ourway.example",
		Username:     "ada",
		DisplayName:  "Ada Lovelace",
		Groups:       []string{"technicians"},
	}
}

// TestSAMLLoginRoundTripIsARealAssertionTheSPAccepts is the happy path, and the
// fixture proof: the SP's deflated request survives the round trip, the IdP's signed
// response validates, and the identity that comes out is the identity asserted —
// subject, email, display name and groups.
func TestSAMLLoginRoundTripIsARealAssertionTheSPAccepts(t *testing.T) {
	f := newSAMLFixture(t)

	requestID := sso.BuildAuthnRequestID()
	requestXML := sso.SAMLAuthnRequest(f.Cfg, requestID, time.Now())
	redirect, err := sso.SPInitiatedRedirect(requestXML, f.Cfg)
	if err != nil {
		t.Fatalf("SPInitiatedRedirect: %v", err)
	}
	if !strings.HasPrefix(redirect, f.Cfg.IdpSSOURL+"?") {
		t.Fatalf("redirect goes to %q, not the provider's SSO location", redirect)
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatalf("redirect URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("Signature") == "" || query.Get("SigAlg") == "" {
		t.Fatalf("the provider has an SP key and the request is unsigned: %v", query)
	}
	// The IdP's side of the round trip: the request must inflate back to the
	// document the SP built, with our entity ID as its issuer and our ACS as its
	// destination.
	inflated, err := sso.InflateSAMLRequest(query.Get("SAMLRequest"))
	if err != nil {
		t.Fatalf("InflateSAMLRequest: %v", err)
	}
	if !strings.Contains(inflated, testSPEntity) || !strings.Contains(inflated, testACS) {
		t.Fatalf("inflated request lost the SP identity or the ACS:\n%s", inflated)
	}

	formValue := f.IdP.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, testACS, requestID, testSPEntity)
	doc, err := sso.ParseSAMLResponse(formValue)
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	identity, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now())
	if refusal != nil {
		t.Fatalf("a conformant signed assertion from the pinned IdP was refused: %v", refusal)
	}
	if identity.Info.Email != "ada@ourway.example" || identity.Info.Subject != "ada@ourway.example" {
		t.Fatalf("identity is %+v, want the asserted NameID as subject and email", identity.Info)
	}
	if !identity.Info.EmailVerified {
		t.Fatal("an email-format NameID is the IdP asserting the address itself: it is verified")
	}
	if identity.Info.Name != "Ada Lovelace" || len(identity.Info.Groups) != 1 || identity.Info.Groups[0] != "technicians" {
		t.Fatalf("asserted attributes did not reach the contract: %+v", identity.Info)
	}
	if identity.AssertionID == "" {
		t.Fatal("the assertion ID is what replay protection remembers: it must be read")
	}

	// The same assertion, presented twice, is one login.
	replay := sso.NewReplayStore()
	if !replay.Mark(identity.AssertionID, time.Now()) {
		t.Fatal("the first presentation of an assertion is consumed")
	}
	if replay.Mark(identity.AssertionID, time.Now()) {
		t.Fatal("the same assertion was consumed twice")
	}
	if !replay.Seen(identity.AssertionID) {
		t.Fatal("a consumed assertion is not remembered")
	}
}

// TestSAMLAssertionsFromTheSignedDocumentAreRefused is 2.4-7's table. Every row
// mints a real assertion and then breaks it in the way the SAML literature says a
// SP must survive.
func TestSAMLAssertionsAreRefusedForTheNamedReason(t *testing.T) {
	cases := []struct {
		name   string
		claims testsaml.Claims
		opts   testsaml.AssertionOptions
		// mutate rewrites the signed document, after the signature was made.
		mutate func(t *testing.T, xml string) string
		// inResponseTo overrides what the response answers.
		inResponseTo string
		expected     string
		contains     string
	}{
		{
			name:     "unsigned assertion",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{NoSignature: true},
			contains: sso.ReasonSAMLUnsigned,
		},
		{
			name:   "tampered after signing",
			claims: samlClaims(),
			opts:   testsaml.AssertionOptions{},
			mutate: func(t *testing.T, xml string) string {
				// The NameID is the identity: swap it inside a document whose
				// signature covers the original bytes.
				return strings.Replace(xml, "ada@ourway.example", "admin@ourway.example", -1)
			},
			contains: sso.ReasonSAMLSignature,
		},
		{
			name:   "assertion names an issuer the provider does not have",
			claims: samlClaims(),
			// The assertion inside a well-formed response names another IdP.
			opts:     testsaml.AssertionOptions{Issuer: "https://other-idp.example/"},
			contains: sso.ReasonSAMLIssuerMismatch,
		},
		{
			name:     "audience names another service provider",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{Audience: "urn:ourway:victim:22222222"},
			contains: sso.ReasonSAMLAudience,
		},
		{
			name:     "recipient is not this installation's ACS",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{Recipient: "https://other.example/api/auth/sso/saml/keycloak/acs"},
			contains: sso.ReasonSAMLRecipient,
		},
		{
			name:     "response is addressed to another destination",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{ResponseDestination: "https://other.example/acs"},
			contains: sso.ReasonSAMLDestination,
		},
		{
			name:         "response answers a request this login did not send",
			claims:       samlClaims(),
			opts:         testsaml.AssertionOptions{},
			inResponseTo: "_someone_elses_request",
			contains:     sso.ReasonSAMLInResponseTo,
		},
		{
			name:   "assertion expired",
			claims: samlClaims(),
			opts: testsaml.AssertionOptions{
				NotOnOrAfter: time.Now().Add(-30 * time.Minute),
			},
			contains: sso.ReasonSAMLNotOnOrAfter,
		},
		{
			name:     "assertion is not valid before",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{NotBefore: time.Now().Add(30 * time.Minute)},
			contains: sso.ReasonSAMLNotValidBefore,
		},
		{
			name:     "authentication is in the future",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{AuthnInstant: time.Now().Add(30 * time.Minute)},
			contains: sso.ReasonSAMLSessionStart,
		},
		{
			name:     "bearer confirmation is absent",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{NoSubjectConfirmation: true},
			contains: sso.ReasonSAMLSubject,
		},
		{
			name:     "bearer confirmation carries no data",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{NoConfirmationData: true},
			contains: sso.ReasonSAMLConfirmation,
		},
		{
			name:     "bearer confirmation never expires",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{NoConfirmationExpiry: true},
			contains: sso.ReasonSAMLConfirmation,
		},
		{
			name:     "confirmation method is not bearer",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{ConfirmationMethod: "urn:oasis:names:tc:SAML:2.0:cm:holder-of-key"},
			contains: sso.ReasonSAMLConfirmation,
		},
		{
			name:     "assertion asserts no NameID",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{NoNameID: true},
			contains: sso.ReasonSAMLSubject,
		},
		{
			name:     "wrapped signature covers another element",
			claims:   samlClaims(),
			opts:     testsaml.AssertionOptions{SignedElementID: "_the_signed_one"},
			contains: sso.ReasonSAMLSignature,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSAMLFixture(t)
			requestID := sso.BuildAuthnRequestID()
			inResponseTo := requestID
			if tc.inResponseTo != "" {
				inResponseTo = tc.inResponseTo
			}
			xml := f.IdP.ResponseXML(tc.claims, tc.opts, testACS, inResponseTo, testSPEntity)
			if tc.mutate != nil {
				xml = tc.mutate(t, xml)
			}
			doc, err := sso.ParseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(xml)))
			if err != nil {
				t.Fatalf("ParseSAMLResponse: %v", err)
			}
			identity, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now())
			if refusal == nil {
				t.Fatalf("the assertion was accepted: %+v", identity)
			}
			if !strings.Contains(refusal.Reason, tc.contains) && !strings.Contains(refusal.Error(), tc.contains) {
				t.Fatalf("refusal is %q (%s), want it to name %q", refusal.Reason, refusal.Detail, tc.contains)
			}
			if tc.expected != "" && refusal.Reason != tc.expected {
				t.Fatalf("reason is %q, want %q", refusal.Reason, tc.expected)
			}
			// The refusal must be a sentence an operator can act on at the IdP.
			if strings.TrimSpace(refusal.Detail) == "" {
				t.Fatal("the refusal names no detail")
			}
		})
	}
}

// TestSAMLResponseSignatureIsRequiredWhenTheProviderAsks covers the flag the
// operator turns on when the IdP signs the Response as well as the assertion.
func TestSAMLResponseSignatureIsRequiredWhenTheProviderAsks(t *testing.T) {
	f := newSAMLFixture(t)
	f.Cfg.RequireResponseSignature = true

	requestID := sso.BuildAuthnRequestID()
	formValue := f.IdP.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, testACS, requestID, testSPEntity)
	doc, err := sso.ParseSAMLResponse(formValue)
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	if _, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now()); refusal == nil ||
		!strings.Contains(refusal.Error(), sso.ReasonSAMLUnsigned) {
		t.Fatalf("a response with an unsigned Response element was accepted: %v", refusal)
	}

	formValue = f.IdP.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{SignResponse: true}, testACS, requestID, testSPEntity)
	doc, err = sso.ParseSAMLResponse(formValue)
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	if identity, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now()); refusal != nil {
		t.Fatalf("the signed response was refused: %v (identity %+v)", refusal, identity)
	}
}

// TestSAMLResponseWithNoAssertionIsRefused: an IdP that answers Success and posts
// nothing, and an IdP that posts two people in one response.
func TestSAMLResponseShapeIsChecked(t *testing.T) {
	f := newSAMLFixture(t)

	// Success with no assertion at all.
	status := f.IdP.StatusResponse(testACS, "", testsaml.StatusSuccess, "")
	doc, err := sso.ParseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(status)))
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	if _, refusal := sso.ValidateResponse(doc, f.Cfg, "", time.Now()); refusal == nil ||
		!strings.Contains(refusal.Error(), sso.ReasonSAMLNoAssertion) {
		t.Fatalf("a Success response with no assertion was accepted: %v", refusal)
	}

	// The IdP refused the login on its own side, and says why.
	status = f.IdP.StatusResponse(testACS, "", "urn:oasis:names:tc:SAML:2.0:status:Responder", "Account locked")
	doc, err = sso.ParseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(status)))
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	_, refusal := sso.ValidateResponse(doc, f.Cfg, "", time.Now())
	if refusal == nil || !strings.Contains(refusal.Reason, sso.ReasonSAMLStatus) {
		t.Fatalf("the IdP's own refusal was not reported: %v", refusal)
	}
	if !strings.Contains(refusal.Detail, "Account locked") {
		t.Fatalf("the IdP's status message was dropped: %s", refusal.Detail)
	}

	// Non-XML and over-long form values are refused before anything is parsed.
	if _, err := sso.ParseSAMLResponse("not base64 at all"); err == nil {
		t.Fatal("a form value that is not base64 is refused")
	}
	oversized := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("<Response/>", 200_000)))
	if _, err := sso.ParseSAMLResponse(oversized); err == nil {
		t.Fatal("a form value over the size cap is refused")
	}
}

// TestSAMLReplayWindowToleratesSkewAndRefusesExpiry is the clock rule: seconds of
// disagreement between the IdP and this installation are absorbed, minutes of
// expired assertion are not.
func TestSAMLReplayWindowToleratesSkewAndRefusesExpiry(t *testing.T) {
	f := newSAMLFixture(t)

	expiredBy := func(t *testing.T, ago time.Duration) *sso.Refusal {
		t.Helper()
		requestID := sso.BuildAuthnRequestID()
		formValue := f.IdP.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{
			NotOnOrAfter: time.Now().Add(-ago),
		}, testACS, requestID, testSPEntity)
		doc, err := sso.ParseSAMLResponse(formValue)
		if err != nil {
			t.Fatalf("ParseSAMLResponse: %v", err)
		}
		_, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now())
		return refusal
	}

	if refusal := expiredBy(t, 30*time.Second); refusal != nil {
		t.Fatalf("an assertion 30 seconds past its window is a clock disagreement, not a refusal: %v", refusal)
	}
	refusal := expiredBy(t, 30*time.Minute)
	if refusal == nil || !strings.Contains(refusal.Reason, sso.ReasonSAMLNotOnOrAfter) {
		t.Fatalf("an assertion 30 minutes past its window was accepted: %v", refusal)
	}
}

// TestSAMLSPMetadataNamesTheEntityAndCarriesNoKey is the document the operator
// uploads to the IdP: it must be parseable, name this provider's entity ID, the ACS
// location, the signing certificate, and it must never carry the private key.
func TestSAMLSPMetadataNamesTheEntityAndCarriesNoKey(t *testing.T) {
	f := newSAMLFixture(t)

	metadata, err := sso.SPMetadata(f.Cfg)
	if err != nil {
		t.Fatalf("SPMetadata: %v", err)
	}
	for _, want := range []string{testSPEntity, testACS, testsaml.NameIDEmail, "WantAssertionsSigned=\"true\"", "AuthnRequestsSigned=\"true\"", "X509Certificate"} {
		if !strings.Contains(metadata, want) {
			t.Fatalf("metadata omits %q:\n%s", want, metadata)
		}
	}
	if strings.Contains(metadata, "PRIVATE KEY") || strings.Contains(metadata, f.Cfg.SPKeyPEM) {
		t.Fatal("the SP metadata carries key material")
	}
	var descriptor struct {
		EntityID   string `xml:"entityID,attr"`
		Descriptor struct {
			WantAssertionsSigned string `xml:"WantAssertionsSigned,attr"`
			ACS                  struct {
				Location string `xml:"Location,attr"`
				Binding  string `xml:"Binding,attr"`
			} `xml:"AssertionConsumerService"`
		} `xml:"SPSSODescriptor"`
	}
	if err := xml.Unmarshal([]byte(metadata), &descriptor); err != nil {
		t.Fatalf("the metadata an IdP must parse does not unmarshal: %v", err)
	}
	if descriptor.EntityID != testSPEntity {
		t.Fatalf("metadata entityID is %q", descriptor.EntityID)
	}
	if descriptor.Descriptor.ACS.Location != testACS {
		t.Fatalf("metadata ACS is %q", descriptor.Descriptor.ACS.Location)
	}

	// The certificate fingerprint is what the console shows, and it is the pinned
	// certificate's own fingerprint.
	fingerprint := sso.CertificateFingerprint(f.Cfg.IdpCertificate)
	if len(fingerprint) != 95 || strings.Count(fingerprint, ":") != 31 {
		t.Fatalf("fingerprint is %q, want 32 colon-separated bytes", fingerprint)
	}
	if sso.CertificateFingerprint("not a certificate") != "" {
		t.Fatal("a certificate that does not parse has no fingerprint")
	}
	if problem := sso.ValidateCertificatePEM(f.Cfg.IdpCertificate, time.Now()); problem != "" {
		t.Fatalf("a valid certificate is refused: %s", problem)
	}
	expired, _, err := sso.GenerateSPKeyPair("urn:expired", time.Now().AddDate(-2, 0, 0))
	if err != nil {
		t.Fatalf("GenerateSPKeyPair: %v", err)
	}
	if problem := sso.ValidateCertificatePEM(expired, time.Now()); problem == "" {
		t.Fatal("an expired certificate is accepted by the save-time check")
	}
}

// TestSAMLIdentityFromAnEmailFormatNameIDFeedsTheContract is the seam 2.4-7 shares
// with 2.4-3: the assertion's NameID becomes the subject the account is filed under,
// the asserted groups reach the role mapping, and a provider that requires a
// verified email reads the attribute the operator named.
func TestSAMLIdentityFromAnEmailFormatNameIDFeedsTheContract(t *testing.T) {
	f := newSAMLFixture(t)

	claims := samlClaims()
	claims.VerifiedAttribute = "emailVerified"
	claims.Verified = false
	f.Cfg.VerifiedEmailAttribute = "emailVerified"

	requestID := sso.BuildAuthnRequestID()
	formValue := f.IdP.ResponseFormValue(claims, testsaml.AssertionOptions{}, testACS, requestID, testSPEntity)
	doc, err := sso.ParseSAMLResponse(formValue)
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	identity, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now())
	if refusal != nil {
		t.Fatalf("the assertion was refused: %v", refusal)
	}
	if identity.Info.EmailVerified {
		t.Fatal("the provider named the attribute that states verification, and the IdP said false")
	}

	claims.Verified = true
	formValue = f.IdP.ResponseFormValue(claims, testsaml.AssertionOptions{}, testACS, requestID, testSPEntity)
	doc, _ = sso.ParseSAMLResponse(formValue)
	identity, refusal = sso.ValidateResponse(doc, f.Cfg, requestID, time.Now())
	if refusal != nil {
		t.Fatalf("the assertion was refused: %v", refusal)
	}
	if !identity.Info.EmailVerified {
		t.Fatal("the IdP asserted the email is verified and the SP did not read it")
	}

	// An assertion whose NameID is not an address, and which names no email
	// attribute, asserts no identity this product can file an account under.
	claims = samlClaims()
	claims.NameID = "ada-lovelace"
	claims.NameIDFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified"
	claims.Email = ""
	claims.VerifiedAttribute = ""
	formValue = f.IdP.ResponseFormValue(claims, testsaml.AssertionOptions{}, testACS, requestID, testSPEntity)
	doc, _ = sso.ParseSAMLResponse(formValue)
	if _, refusal := sso.ValidateResponse(doc, f.Cfg, requestID, time.Now()); refusal == nil ||
		!strings.Contains(refusal.Reason, sso.ReasonSAMLSubject) {
		t.Fatalf("an assertion with no email was accepted: %v", refusal)
	}

	// The attribute names are the provider's: an IdP that calls the group attribute
	// `memberOf` is configured, not patched.
	f.Cfg.AttrGroups = "memberOf"
	f.Cfg.AttrName = "givenName"
	f.Cfg.AttrEmail = "mail"
	claims = samlClaims()
	claims.Email = ""
	claims.DisplayName = ""
	claims.GroupsAttribute = "memberOf"
	claims.ExtraAttributes = [][]string{{"mail", "ada@ourway.example"}, {"givenName", "Ada"}}
	formValue = f.IdP.ResponseFormValue(claims, testsaml.AssertionOptions{}, testACS, requestID, testSPEntity)
	doc, _ = sso.ParseSAMLResponse(formValue)
	identity, refusal = sso.ValidateResponse(doc, f.Cfg, requestID, time.Now())
	if refusal != nil {
		t.Fatalf("the provider's own attribute names were not read: %v", refusal)
	}
	if identity.Info.Email != "ada@ourway.example" {
		t.Fatalf("email came from %q, not the configured attribute", identity.Info.Email)
	}
	if len(identity.Info.Groups) != 1 || identity.Info.Groups[0] != "technicians" {
		t.Fatalf("groups did not reach the contract: %+v", identity.Info)
	}
}

// TestSAMLDeflateRoundTrip is the encoding the HTTP-Redirect binding requires:
// raw deflate, base64, no zlib header.
func TestSAMLDeflateRoundTrip(t *testing.T) {
	f := newSAMLFixture(t)
	request := sso.SAMLAuthnRequest(f.Cfg, sso.BuildAuthnRequestID(), time.Now())
	encoded, err := sso.DeflateSAMLRequest(request)
	if err != nil {
		t.Fatalf("DeflateSAMLRequest: %v", err)
	}
	if strings.Contains(encoded, "\n") {
		t.Fatal("the encoded request must be a single form value")
	}
	inflated, err := sso.InflateSAMLRequest(encoded)
	if err != nil {
		t.Fatalf("InflateSAMLRequest: %v", err)
	}
	if inflated != request {
		t.Fatal("the request the IdP inflates is not the request the SP sent")
	}
}

// TestSAMLAssertionSignedByAnUnpinnedKeyIsRefused is the trust-anchor rule: the
// certificate the operator pinned is the only key whose assertions this installation
// accepts. A document minted by a *different* IdP — a key OurWay was given, a leaked
// or superseded key, an IdP that rotated its certificate without telling the SP —
// asserts the same issuer, the same audience, the same recipient, and is refused.
//
// This is also the shape of a real operational event: the IdP rotates its signing
// key, and every login starts failing with a refusal that names the fingerprint the
// operator has to update.
func TestSAMLAssertionSignedByAnUnpinnedKeyIsRefused(t *testing.T) {
	pinned := newSAMLFixture(t)
	other := testsaml.New()

	requestID := sso.BuildAuthnRequestID()
	formValue := other.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{
		Issuer:         pinned.Cfg.IdpEntityID,
		ResponseIssuer: pinned.Cfg.IdpEntityID,
	}, testACS, requestID, testSPEntity)
	doc, err := sso.ParseSAMLResponse(formValue)
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	_, refusal := sso.ValidateResponse(doc, pinned.Cfg, requestID, time.Now())
	if refusal == nil {
		t.Fatal("an assertion signed by a key the provider does not pin was accepted")
	}
	if !strings.Contains(refusal.Reason, sso.ReasonSAMLSignature) {
		t.Fatalf("refusal is %q (%s), want it to name %q", refusal.Reason, refusal.Detail, sso.ReasonSAMLSignature)
	}
	if !strings.Contains(refusal.Detail, sso.CertificateFingerprint(pinned.Cfg.IdpCertificate)) {
		t.Fatalf("the refusal does not name the pinned certificate's fingerprint, which is the value the operator must update: %s", refusal.Detail)
	}

	// The same document validates once the operator pins the key that signed it:
	// pinning is the configuration act, and it is the only one.
	pinned.Cfg.IdpCertificate = other.CertificatePEM()
	if identity, refusal := sso.ValidateResponse(doc, pinned.Cfg, requestID, time.Now()); refusal != nil {
		t.Fatalf("the assertion was refused after the operator pinned the signing key: %v", refusal)
	} else if identity.Info.Email != "ada@ourway.example" {
		t.Fatalf("identity is %+v", identity.Info)
	}
}
