package sso_test

// The vendor interop probe (2.4-0): a captured SAML Response from a **real IdP** run
// through OurWay's verifier.
//
// Everything else in this suite proves the rules against `testsaml`, a peer IdP we
// wrote. A document our own signer mints can only ever agree with our own verifier:
// the shapes that separate a working SAML SP from a theoretical one — a Response that
// declares its namespaces once at the root, an assertion that resolves its prefixes
// through that declaration, a persistent-format NameID with the email riding in an
// attribute — are the shapes a vendor produces, and only a capture proves we read them.
//
// The capture therefore comes from outside the suite, through the environment, and the
// test skips when it is absent. The suite stays hermetic; the pass stays re-runnable by
// anyone who has a container and a browser.
//
// Environment:
//
//	OURWAY_SAML_CAPTURE      file holding the `SAMLResponse` form value (base64) or raw XML
//	OURWAY_SAML_CERT         the pinned certificate the operator pasted in (PEM, armored or not)
//	OURWAY_SAML_REQUEST_ID   the AuthnRequest ID the captured response answers
//	OURWAY_SAML_AT           the SP's clock for the validation, ISO-8601 (default: now)
//	OURWAY_SAML_SP           the SP entity ID the assertion must name   (default: the interop SP)
//	OURWAY_SAML_ACS          the ACS location the assertion must be addressed to
//	OURWAY_SAML_IDP_ENTITY   the IdP entity ID the assertion must be issued by
//	OURWAY_SAML_IDP_SSO      the IdP's SSO location, for the redirect proof
//	OURWAY_SAML_ATTR_EMAIL   the attribute that carries the email address
//
// The defaults name the 2.4-0 interop installation (Keycloak 26 realm `ourway` at
// kc.sso.test, OurWay at ow.sso.test), because that is the capture this pass was
// proven with; every one of them is overridable for the next vendor.

import (
	"encoding/base64"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"

	"ourway/server/sso"
)

const (
	vendorCanonicalization = "http://www.w3.org/2001/10/xml-exc-c14n#"
	interopIDPEntity       = "https://kc.sso.test/realms/ourway"
	interopIDPSSO          = "https://kc.sso.test/realms/ourway/protocol/saml"
	interopSP              = "urn:ourway:kc-saml:947f7a7e-b5e6-4054-8d75-5cdae45690ab"
	interopACS             = "https://ow.sso.test/api/auth/sso/saml/kc-saml/acs"
)

// TestAVendorCaptureValidatesRefusesTamperingAndRefusesAnUnsignedAssertion is the
// interop proof in one test: the vendor's own document validates, one changed byte in
// the NameID makes it refuse, and stripping the signature makes it refuse *as unsigned*
// — the two refusals an operator needs to be able to tell apart.
func TestAVendorCaptureValidatesRefusesTamperingAndRefusesAnUnsignedAssertion(t *testing.T) {
	path := os.Getenv("OURWAY_SAML_CAPTURE")
	if path == "" {
		t.Skip("no vendor capture: set OURWAY_SAML_CAPTURE (plus OURWAY_SAML_CERT and OURWAY_SAML_REQUEST_ID)")
	}
	document := vendorDocument(t, path)
	requestID := vendorEnv("OURWAY_SAML_REQUEST_ID", "")
	if requestID == "" {
		t.Fatal("OURWAY_SAML_REQUEST_ID is required: the capture must answer a login the SP started")
	}
	pinned := vendorCertificate(t)
	if pinned == "" {
		t.Fatal("OURWAY_SAML_CERT is required: the pinned certificate is the only trust anchor")
	}
	if block, _ := pem.Decode([]byte(pemArmor(pinned))); block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("the pinned certificate is not a PEM certificate: paste the IdP console's text, or pass the file")
	}
	at := vendorClock(t)
	cfg := vendorConfig(t)

	// The shape guard. This test proves exclusive canonicalization over a subtree whose
	// prefixes resolve through an enclosing document; a capture that declares everything
	// on the assertion proves nothing, so it is refused as evidence here.
	assertVendorShape(t, document)

	// The canonicalization algorithm the IdP says it used. Our verifier implements the
	// exclusive one; a capture signed with inclusive c14n is a different proof, and
	// silently passing it would let the suite believe it covered this one.
	if method := vendorCanonicalizationMethod(t, document); method != vendorCanonicalization {
		t.Fatalf("the capture is canonicalized with %q, not the exclusive form this proof is about", method)
	}

	// The window the capture itself asserts: the proof must run inside it, and naming
	// it is the point — the document has a known expiry, and OURWAY_SAML_AT is chosen
	// against that, not against the wall clock.
	expiry := vendorEarliestExpiry(t, document)
	if expiry.Before(at) {
		t.Fatalf("OURWAY_SAML_AT %s is past the capture's own expiry %s: the clean case proves expiry, not validation",
			at.Format(time.RFC3339), expiry.Format(time.RFC3339))
	}

	identity, refusal := validateVendor(t, document, cfg, requestID, at)
	if refusal != nil {
		t.Fatalf("the vendor's own document was refused: %v\n%s", refusal, vendorExcerpt(document))
	}
	t.Logf("the captured vendor assertion VALIDATES: subject=%s email=%s groups=%v issuer=%s assertion=%s",
		identity.Info.Subject, identity.Info.Email, identity.Info.Groups, identity.Issuer, identity.AssertionID)
	if identity.Info.Subject == "" {
		t.Fatal("the identity has no subject: the persistent-format NameID was not read")
	}

	// One byte inside the asserted NameID. The signature must notice.
	tampered := vendorTamperNameID(t, document)
	if _, refusal := validateVendor(t, tampered, cfg, requestID, at); refusal == nil {
		t.Fatal("a tampered vendor assertion validated: the signature is not being verified")
	} else if refusal.Reason != sso.ReasonSAMLSignature {
		t.Fatalf("tampered capture refused as %s (%v), want %s", refusal.Reason, refusal, sso.ReasonSAMLSignature)
	} else {
		t.Logf("tampered vendor assertion refused: %s", refusal.Reason)
	}

	// No signature at all: a distinct refusal, because "the IdP is not signing" and
	// "the signature does not verify" are different fixes on the IdP side.
	unsigned := vendorStripSignature(t, document)
	if _, refusal := validateVendor(t, unsigned, cfg, requestID, at); refusal == nil {
		t.Fatal("an unsigned vendor assertion validated: unsigned identities are accepted")
	} else if refusal.Reason != sso.ReasonSAMLUnsigned {
		t.Fatalf("unsigned capture refused as %s (%v), want %s", refusal.Reason, refusal, sso.ReasonSAMLUnsigned)
	} else {
		t.Logf("unsigned vendor assertion refused: %s", refusal.Reason)
	}
}

// vendorDocument reads the capture as the XML inside the form value: the base64 the
// browser posts, or the document itself when a debugging capture was saved raw.
func vendorDocument(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	text := strings.TrimSpace(string(raw))
	if decoded, err := base64.StdEncoding.DecodeString(text); err == nil && strings.HasPrefix(strings.TrimSpace(string(decoded)), "<") {
		return strings.TrimSpace(string(decoded))
	}
	if !strings.HasPrefix(text, "<") {
		t.Fatalf("%s is neither a base64 SAMLResponse nor an XML document", path)
	}
	return text
}

func vendorEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// vendorCertificate reads the pinned certificate from a path when the environment names
// one, and takes the PEM inline otherwise: an operator pastes the console's text, a
// harness passes the file it saved.
func vendorCertificate(t *testing.T) string {
	t.Helper()
	value := vendorEnv("OURWAY_SAML_CERT", "")
	if value == "" {
		return ""
	}
	if raw, err := os.ReadFile(value); err == nil {
		return strings.TrimSpace(string(raw))
	}
	return value
}

// pemArmor accepts a capture's certificate with or without the armor lines, since the
// IdP console and the browser's download give the two shapes.
func pemArmor(text string) string {
	if strings.Contains(text, "-----BEGIN CERTIFICATE-----") {
		return text
	}
	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	if err != nil {
		// The certificate check at the top of the test names this; a probe helper does
		// not get to decide the run is unwinnable.
		return text
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: body}))
}

// vendorClock is the SP's clock for the validation. A vendor capture is a fixed
// document with a fixed expiry, so the clock is part of the evidence.
func vendorClock(t *testing.T) time.Time {
	t.Helper()
	value := vendorEnv("OURWAY_SAML_AT", "")
	if value == "" {
		return time.Now().UTC()
	}
	for _, layout := range []string{"2006-01-02T15:04:05Z", time.RFC3339} {
		if at, err := time.Parse(layout, value); err == nil {
			return at.UTC()
		}
	}
	t.Fatalf("OURWAY_SAML_AT %q is not an ISO-8601 time", value)
	return time.Now()
}

func vendorConfig(t *testing.T) sso.SAMLConfig {
	t.Helper()
	provider := vendorEnv("OURWAY_SAML_PROVIDER", "kc-saml")
	return sso.SAMLConfig{
		ProviderName: provider,
		ProviderID:   provider,
		// The IdP's own addresses, so the checks this proof is about are the checks
		// the vendor has to pass.
		IdpEntityID:              vendorEnv("OURWAY_SAML_IDP_ENTITY", interopIDPEntity),
		IdpSSOURL:                vendorEnv("OURWAY_SAML_IDP_SSO", interopIDPSSO),
		IdpCertificate:           pemArmor(vendorCertificate(t)),
		SPEntityID:               vendorEnv("OURWAY_SAML_SP", interopSP),
		ACSURL:                   vendorEnv("OURWAY_SAML_ACS", interopACS),
		RequireResponseSignature: true,
		NameIDFormat:             vendorEnv("OURWAY_SAML_NAMEID_FORMAT", "persistent"),
		AttrEmail:                vendorEnv("OURWAY_SAML_ATTR_EMAIL", "email"),
		AttrUsername:             vendorEnv("OURWAY_SAML_ATTR_USERNAME", "username"),
		AttrName:                 vendorEnv("OURWAY_SAML_ATTR_NAME", "given.name"),
		AttrGroups:               vendorEnv("OURWAY_SAML_ATTR_GROUPS", "Role"),
	}
}

// validateVendor runs one serialization of the capture through the same entry point
// the ACS handler uses.
func validateVendor(t *testing.T, document string, cfg sso.SAMLConfig, requestID string, at time.Time) (*sso.Identity, *sso.Refusal) {
	t.Helper()
	doc, err := sso.ParseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(document)))
	if err != nil {
		t.Fatalf("ParseSAMLResponse: %v", err)
	}
	return sso.ValidateResponse(doc, cfg, requestID, at)
}

// assertVendorShape fails when the capture is not the shape the proof is about: the
// namespaces declared once on the Response, the assertion carrying only the default
// declaration for its own namespace, and every `saml:`-prefixed element inside the
// signed subtree resolving through the enclosing document.
func assertVendorShape(t *testing.T, document string) {
	t.Helper()
	root := parseVendor(t, document)
	assertion := findVendorElement(root, "Assertion")
	if assertion == nil {
		t.Fatal("the capture has no Assertion element")
	}
	defaults, prefixed := 0, 0
	for _, attr := range assertion.Attr {
		switch {
		case attr.Space == "" && attr.Key == "xmlns":
			defaults++
		case attr.Space == "xmlns":
			prefixed++
		}
	}
	if prefixed != 0 {
		t.Fatalf("the assertion declares %d prefixed namespace(s) of its own: this capture does not test in-scope namespaces", prefixed)
	}
	if defaults == 0 {
		t.Fatal("the assertion declares no default namespace and no prefixed one: nothing here resolves through an ancestor")
	}
	responseSaml := false
	for _, attr := range root.Attr {
		if attr.Space == "xmlns" && attr.Key == "saml" {
			responseSaml = true
		}
	}
	if !responseSaml {
		t.Fatal("the Response does not declare the saml prefix the assertion's elements use")
	}
}

func vendorCanonicalizationMethod(t *testing.T, document string) string {
	t.Helper()
	root := parseVendor(t, document)
	if signature := findVendorElement(root, "Signature"); signature != nil {
		if method := findVendorElement(signature, "CanonicalizationMethod"); method != nil {
			for _, attr := range method.Attr {
				if attr.Key == "Algorithm" {
					return attr.Value
				}
			}
		}
	}
	return ""
}

// vendorEarliestExpiry is the tightest NotOnOrAfter the capture asserts, so the test
// can say which window the clean case was proven inside.
func vendorEarliestExpiry(t *testing.T, document string) time.Time {
	t.Helper()
	earliest := time.Time{}
	for _, attr := range vendorAttrValues(document, "NotOnOrAfter") {
		at, err := time.Parse("2006-01-02T15:04:05Z", attr)
		if err != nil {
			continue
		}
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	}
	if earliest.IsZero() {
		t.Fatal("the capture asserts no NotOnOrAfter: its expiry window is unknown")
	}
	return earliest
}

func vendorAttrValues(document, key string) []string {
	var out []string
	for _, line := range strings.Split(document, "<") {
		if index := strings.Index(line, key+`="`); index >= 0 {
			rest := line[index+len(key)+2:]
			if end := strings.Index(rest, `"`); end >= 0 {
				out = append(out, rest[:end])
			}
		}
	}
	return out
}

// vendorTamperNameID changes one byte inside the asserted NameID text — the identity
// the assertion is about — and leaves everything else, including the signature, alone.
func vendorTamperNameID(t *testing.T, document string) string {
	t.Helper()
	root := parseVendor(t, document)
	nameID := findVendorElement(root, "NameID")
	if nameID == nil {
		t.Fatal("the capture asserts no NameID to tamper with")
	}
	nameID.SetText(nameID.Text() + "x")
	return writeVendor(t, root)
}

// vendorStripSignature removes the assertion's enveloped signature, leaving a document
// that is otherwise exactly what the IdP posted.
func vendorStripSignature(t *testing.T, document string) string {
	t.Helper()
	root := parseVendor(t, document)
	assertion := findVendorElement(root, "Assertion")
	if assertion == nil {
		t.Fatal("the capture has no Assertion to strip")
	}
	for i, child := range assertion.Child {
		if element, ok := child.(*etree.Element); ok && element.Tag == "Signature" {
			assertion.RemoveChildAt(i)
			return writeVendor(t, root)
		}
	}
	t.Fatal("the captured assertion carries no Signature: the capture is not the signed shape")
	return ""
}

func parseVendor(t *testing.T, document string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(document); err != nil {
		t.Fatalf("the capture does not parse as XML: %v", err)
	}
	if doc.Root() == nil {
		t.Fatal("the capture has no root element")
	}
	return doc.Root()
}

func writeVendor(t *testing.T, root *etree.Element) string {
	t.Helper()
	doc := etree.NewDocument()
	doc.SetRoot(root)
	out, err := doc.WriteToString()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return out
}

// findVendorElement walks for the first element with this local name, whatever prefix
// the IdP wrote it with.
func findVendorElement(el *etree.Element, name string) *etree.Element {
	if el == nil {
		return nil
	}
	if el.Tag == name {
		return el
	}
	for _, child := range el.ChildElements() {
		if found := findVendorElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func vendorExcerpt(document string) string {
	if len(document) > 600 {
		return document[:600] + "…"
	}
	return document
}
