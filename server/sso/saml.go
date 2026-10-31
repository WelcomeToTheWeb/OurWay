package sso

import (
	"bytes"
	"compress/flate"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/beevik/etree"
	"github.com/google/uuid"
	dsig "github.com/russellhaering/goxmldsig"

	"ourway/server/models"
)

// SAML 2.0 (2.4-7): the second protocol that OurWay's identity contract serves.
//
// The plan ordered SAML after generic OIDC on purpose, and the split shows in this
// file. Nothing about the *identity decision* is here — domain allow-list, verified
// email, pending-by-default and opt-in role mapping all live in `contract.go` and are
// reached through the same `resolveAccount` the OIDC path uses. What is here is the
// part SAML adds: an assertion that arrives on a POSTed form, whose authority comes
// from an XML signature the SP must verify against the certificate **the operator
// pinned**, and a set of conditions (audience, recipient, destination, expiry,
// subject confirmation) that say whether *this* assertion is for *this* login at
// *this* installation.
//
// The refusals below are named for the same reason 2.4-2's OIDC refusals are: an
// operator staring at "SSO login failed" cannot fix an IdP, and five distinct
// assertion problems are five distinct fixes.

const (
	nsAssertion = "urn:oasis:names:tc:SAML:2.0:assertion"
	nsProtocol  = "urn:oasis:names:tc:SAML:2.0:protocol"
	nsMetadata  = "urn:oasis:names:tc:SAML:2.0:metadata"
	nsDS        = "http://www.w3.org/2000/09/xmldsig#"
	nsXSI       = "http://www.w3.org/2001/XMLSchema-instance"

	bindingPOST     = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
	bindingRedirect = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"

	statusSuccess = "urn:oasis:names:tc:SAML:2.0:status:Success"

	nameIDFormatEmail = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"

	samlTimeLayout = "2006-01-02T15:04:05Z"

	// sigAlgRSA_SHA256 is the signature algorithm the HTTP-Redirect binding names
	// when the SP signs its request.
	sigAlgRSA_SHA256 = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"

	// maxAssertionBytes caps the document the SP will parse. The form is an
	// unauthenticated request body from a browser: size is the first gate.
	maxAssertionBytes = 1 << 20
)

// Assertion refusal reasons. `unsigned` and `signature` are deliberately separate:
// "the IdP is not signing at all" and "the signature does not verify against the
// pinned certificate" are different fixes on the IdP side.
const (
	ReasonSAMLStatus         = "status_refused"
	ReasonSAMLUnparseable    = "assertion_unparseable"
	ReasonSAMLIssuerMismatch = "issuer_mismatch"
	ReasonSAMLDestination    = "destination_mismatch"
	ReasonSAMLInResponseTo   = "in_response_to_mismatch"
	ReasonSAMLNoAssertion    = "no_assertion"
	ReasonSAMLUnsigned       = "assertion_unsigned"
	ReasonSAMLSignature      = "assertion_signature_invalid"
	ReasonSAMLResponseSig    = "response_signature_invalid"
	ReasonSAMLNotOnOrAfter   = "response_expired"
	ReasonSAMLNotValidBefore = "response_not_yet_valid"
	ReasonSAMLAudience       = "audience_mismatch"
	ReasonSAMLRecipient      = "recipient_mismatch"
	ReasonSAMLConfirmation   = "subject_confirmation_method"
	ReasonSAMLSessionStart   = "session_start_in_future"
	ReasonSAMLReplayed       = "assertion_replayed"
	ReasonSAMLSubject        = "no_identity_claim"
)

// samlClockSkew is the tolerance applied to every SAML timestamp. IdPs and SPs
// disagree by seconds in the real world, and an installation whose NTP is 3 seconds
// off must not refuse every login. It is a tolerance, not an extension: an assertion
// expired by more than this is refused.
const samlClockSkew = 90 * time.Second

// samlReplayTTL is how long a consumed assertion ID is remembered. It is derived
// from the expiry window the SP accepts (assertion lifetime + skew), so an
// assertion cannot be replayed at a moment when it would still validate.
const samlReplayTTL = 12 * time.Minute

// SAMLConfig is everything the SAML path needs from a provider row. It is passed
// explicitly, not read from the database inside this package: the SAML code must be
// testable with a provider the test wrote down.
type SAMLConfig struct {
	ProviderName   string
	ProviderID     string
	IdpEntityID    string
	IdpSSOURL      string
	IdpLogoutURL   string
	IdpCertificate string // PEM, as the IdP publishes it

	SPEntityID   string
	ACSURL       string
	SPCertPEM    string
	SPKeyPEM     string
	SignRequests bool

	RequireResponseSignature bool
	NameIDFormat             string

	AttrEmail              string
	AttrUsername           string
	AttrName               string
	AttrGroups             string
	VerifiedEmailAttribute string
}

// SAMLConfigOf projects a provider row onto the SAML path's config.
func SAMLConfigOf(provider *models.SSOProvider, acsURL string) SAMLConfig {
	name := func(value, fallback string) string {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		return fallback
	}
	return SAMLConfig{
		ProviderName:             provider.Name,
		ProviderID:               provider.ID,
		IdpEntityID:              strings.TrimSpace(provider.IdpEntityID),
		IdpSSOURL:                strings.TrimSpace(provider.IdpSSOURL),
		IdpLogoutURL:             strings.TrimSpace(provider.IdpLogoutURL),
		IdpCertificate:           provider.IdpCertificate,
		SPEntityID:               strings.TrimSpace(provider.SPEntityID),
		ACSURL:                   acsURL,
		SPCertPEM:                provider.SPCertPEM,
		SPKeyPEM:                 provider.SPKeyPEM,
		SignRequests:             strings.TrimSpace(provider.SPKeyPEM) != "",
		RequireResponseSignature: provider.RequireResponseSignature,
		NameIDFormat:             name(provider.NameIDFormat, models.SAMLNameIDFormatEmail),
		AttrEmail:                name(provider.AttrEmail, "email"),
		AttrUsername:             name(provider.AttrUsername, "username"),
		AttrName:                 name(provider.AttrName, "displayName"),
		AttrGroups:               name(provider.AttrGroups, "groups"),
		VerifiedEmailAttribute:   strings.TrimSpace(provider.VerifiedEmailAttribute),
	}
}

// Refusal is one SAML validation refusal: a named reason and the sentence an
// operator reads. `Detail` is what failed, not a stack trace.
type Refusal struct {
	Reason string
	Detail string
}

func (r *Refusal) Error() string { return r.Reason + ": " + r.Detail }

func refuse(reason, format string, args ...any) *Refusal {
	return &Refusal{Reason: reason, Detail: fmt.Sprintf(format, args...)}
}

// ParseSAMLResponse decodes the `SAMLResponse` form value into an XML document.
// Decoding is the only place the base64 form is touched, so a document that reached
// validation was decoded from a form value and nothing else.
func ParseSAMLResponse(formValue string) (*etree.Document, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(formValue))
	if err != nil {
		return nil, refuse(ReasonSAMLUnparseable, "SAMLResponse is not base64: %v", err)
	}
	if len(raw) > maxAssertionBytes {
		return nil, refuse(ReasonSAMLUnparseable, "SAMLResponse is %d bytes, over the %d byte cap", len(raw), maxAssertionBytes)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, refuse(ReasonSAMLUnparseable, "SAMLResponse is not XML: %v", err)
	}
	root := doc.Root()
	if root == nil || root.Tag != "Response" {
		return nil, refuse(ReasonSAMLUnparseable, "the document's root is %q, not Response", tagOf(root))
	}
	return doc, nil
}

// Identity is what a validated assertion says about a person, in the same shape the
// OIDC path produces, so the 2.4-3 contract is reached through one door.
type Identity struct {
	Info         *UserInfo
	AssertionID  string
	Issuer       string
	SessionIndex string
	// NotOnOrAfter is the assertion's own expiry, which the replay window is
	// measured against.
	NotOnOrAfter time.Time
}

// ValidateResponse validates a decoded Response against a provider config and
// returns the identity it asserts. Every check is applied; the first refusal wins,
// because these conditions are not a score.
//
// `expectedRequestID` is the ID of the AuthnRequest this login sent: an assertion
// that answers no request of ours (or answers a different one) is not this login's.
func ValidateResponse(doc *etree.Document, cfg SAMLConfig, expectedRequestID string, now time.Time) (*Identity, *Refusal) {
	root := doc.Root()
	// Namespaces are resolved once, for the whole document: every check below
	// matches elements by the namespace URI they are written in.
	issues := newSamlTree(root)

	// 1. The IdP's own answer about the login.
	if refusal := issues.checkStatus(root); refusal != nil {
		return nil, refusal
	}

	// 2. Who signed this document, and who it was addressed to. A response from a
	//    different IdP than the one the operator configured is not this provider's
	//    login, whatever it asserts.
	issuer := issues.text(root, nsAssertion, "Issuer")
	if strings.TrimSpace(issuer) != cfg.IdpEntityID {
		return nil, refuse(ReasonSAMLIssuerMismatch,
			"the assertion names issuer %q; provider %s is configured for %q", issuer, cfg.ProviderName, cfg.IdpEntityID)
	}
	if destination := attrValue(root, "Destination"); destination != cfg.ACSURL {
		return nil, refuse(ReasonSAMLDestination,
			"the response is addressed to %q; this installation's assertion consumer service is %q", destination, cfg.ACSURL)
	}
	if inResponseTo := attrValue(root, "InResponseTo"); inResponseTo != "" {
		if expectedRequestID == "" || inResponseTo != expectedRequestID {
			return nil, refuse(ReasonSAMLInResponseTo,
				"the response answers request %q; this login sent %q", inResponseTo, expectedRequestID)
		}
	} else if expectedRequestID != "" {
		// An SP-initiated login that comes back without InResponseTo did not answer
		// our request: it is either an IdP-initiated post or a captured document
		// being re-posted.
		return nil, refuse(ReasonSAMLInResponseTo,
			"this login sent request %q and the response answers no request", expectedRequestID)
	}

	// 3. Response-level expiry.
	if notOnOrAfter := attrValue(root, "NotOnOrAfter"); notOnOrAfter != "" {
		expiry, err := parseSAMLLocalTime(notOnOrAfter)
		if err != nil {
			return nil, refuse(ReasonSAMLUnparseable, "response NotOnOrAfter is not a SAML timestamp: %v", err)
		}
		if !now.Before(expiry.Add(samlClockSkew)) {
			return nil, refuse(ReasonSAMLNotOnOrAfter, "the response expired at %s", notOnOrAfter)
		}
	}

	assertions := issues.children(root, nsAssertion, "Assertion")
	if len(assertions) == 0 {
		return nil, refuse(ReasonSAMLNoAssertion, "the response carries no assertion")
	}
	if len(assertions) > 1 {
		// Two assertions can assert two people. Choosing one is OurWay inventing an
		// identity, so it refuses.
		return nil, refuse(ReasonSAMLNoAssertion,
			"the response carries %d assertions; one login arrives as one identity", len(assertions))
	}
	assertion := assertions[0]

	// The assertion's own Issuer is the party that signed it: an assertion inserted
	// into a well-formed response by a *different* IdP — one whose certificate this
	// provider does not pin — must not be consumed under this provider's contract.
	// This is also the check that makes a signed-by-another-key refusal read as
	// "issuer mismatch" when the IdP names an issuer we never configured.
	if assertionIssuer := issues.text(assertion, nsAssertion, "Issuer"); assertionIssuer != cfg.IdpEntityID {
		return nil, refuse(ReasonSAMLIssuerMismatch,
			"the assertion names issuer %q; provider %s is configured for %q", assertionIssuer, cfg.ProviderName, cfg.IdpEntityID)
	}

	// 4. Signatures. The assertion signature is mandatory: an unsigned assertion is
	//    an identity anyone can forge by POSTing a form. The response signature is
	//    mandatory when the operator asked for it.
	cert, err := parsePEMCertificate(cfg.IdpCertificate)
	if err != nil {
		return nil, refuse(ReasonSAMLUnsigned, "provider %s has no usable IdP signing certificate: %v", cfg.ProviderName, err)
	}
	if refusal := verifySignature(issues, assertion, cfg, cert, "assertion", now); refusal != nil {
		return nil, refusal
	}
	if refusal := checkSignatureReference(issues, assertion, cfg); refusal != nil {
		return nil, refusal
	}
	if cfg.RequireResponseSignature {
		if refusal := verifySignature(issues, root, cfg, cert, "response", now); refusal != nil {
			return nil, refusal
		}
	}

	// 5. Conditions: the window, the audience, and what the Subject may be
	//    confirmed by.
	subject := issues.child(assertion, nsAssertion, "Subject")
	if subject == nil {
		return nil, refuse(ReasonSAMLSubject, "the assertion has no Subject")
	}
	if refusal := issues.checkSubjectConfirmation(subject, cfg.ACSURL, now); refusal != nil {
		return nil, refusal
	}
	if conditions := issues.child(assertion, nsAssertion, "Conditions"); conditions != nil {
		if refusal := issues.checkConditions(conditions, cfg, now); refusal != nil {
			return nil, refusal
		}
	}

	assertionID := attrValue(assertion, "ID")
	notAfter := time.Time{}
	if raw := attrValue(assertion, "NotOnOrAfter"); raw != "" {
		parsed, err := parseSAMLLocalTime(raw)
		if err != nil {
			return nil, refuse(ReasonSAMLUnparseable, "assertion NotOnOrAfter is not a SAML timestamp: %v", err)
		}
		notAfter = parsed
		if !now.Before(parsed.Add(samlClockSkew)) {
			return nil, refuse(ReasonSAMLNotOnOrAfter, "the assertion expired at %s", raw)
		}
	}

	// 6. AuthnStatement: the session's start and length.
	if statement := issues.child(assertion, nsAssertion, "AuthnStatement"); statement != nil {
		if authnDate := attrValue(statement, "AuthnInstant"); authnDate != "" {
			instant, err := parseSAMLLocalTime(authnDate)
			if err == nil && instant.After(now.Add(samlClockSkew)) {
				return nil, refuse(ReasonSAMLSessionStart, "the assertion says the authentication happened at %s", authnDate)
			}
		}
	}

	info, refusal := issues.identity(assertion, cfg)
	if refusal != nil {
		return nil, refusal
	}

	return &Identity{
		Info:         info,
		AssertionID:  assertionID,
		Issuer:       issuer,
		SessionIndex: issues.text(assertion, nsAssertion, "AuthnStatement", "SessionIndex"),
		NotOnOrAfter: notAfter,
	}, nil
}

// checkStatus reads the response's status code. An IdP that answers "Responder"
// with a status message is telling us the login failed on its side; naming that
// message is the point of carrying it.
func (t *samlTree) checkStatus(root *etree.Element) *Refusal {
	code := ""
	message := ""
	if status := t.child(root, nsProtocol, "Status"); status != nil {
		if codeElement := t.child(status, nsProtocol, "StatusCode"); codeElement != nil {
			code = attrValue(codeElement, "Value")
		}
		message = t.text(status, nsProtocol, "StatusMessage")
	}
	if code == statusSuccess {
		return nil
	}
	if message != "" {
		return refuse(ReasonSAMLStatus, "the IdP refused the login: %s (%s)", code, message)
	}
	return refuse(ReasonSAMLStatus, "the IdP refused the login: %s", code)
}

// verifySignature verifies an enveloped XML signature over one element against the
// pinned certificate. The pinned certificate is the only trust anchor: a signature
// that verifies against a *different* key is refused, which is what makes an IdP key
// rotation a configuration act the operator takes, not a silent widening of who can
// sign assertions for this installation.
func verifySignature(t *samlTree, el *etree.Element, cfg SAMLConfig, cert *x509.Certificate, label string, now time.Time) *Refusal {
	if t.findSignature(el) == nil {
		return refuse(ReasonSAMLUnsigned,
			"the %s from provider %s is not signed; OurWay does not accept an unsigned identity", label, cfg.ProviderName)
	}
	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	ctx := dsig.NewDefaultValidationContext(store)
	// The pinned certificate is the only trust anchor, and its own validity window
	// is checked against the login's time: a certificate that expired is a provider
	// whose assertions nobody can trust any more, which is a fact the operator has
	// to be told, not a login that fails with a parse error.
	ctx.Clock = dsig.NewFakeClockAt(now)
	// Exclusive canonicalization of a signed subtree carries the namespaces that are
	// **in scope for it**, not merely the ones its own element declares. Keycloak
	// declares `xmlns:saml` on the Response and signs an assertion whose every element
	// is prefixed through it; canonicalizing that subtree on its own drops the
	// declaration, the bytes are not the ones the IdP digested, and the signature is
	// refused over a document that is perfectly valid. So the in-scope declarations go
	// onto the element before the digest is taken.
	t.declareInScopeNamespaces(el)
	if _, err := ctx.Validate(el); err != nil {
		return refuse(ReasonSAMLSignature,
			"the %s signature does not verify against provider %s's pinned certificate (%s): %v",
			label, cfg.ProviderName, CertificateFingerprint(cfg.IdpCertificate), err)
	}
	return nil
}

// findSignature reports whether the element carries a ds:Signature child.
func (t *samlTree) findSignature(el *etree.Element) *etree.Element {
	return t.child(el, nsDS, "Signature")
}

// declareInScopeNamespaces copies the namespace declarations in scope for el (its
// own plus every ancestor's) onto el. It is the subtree-only view exclusive
// canonicalization takes: the signed bytes an IdP computed for a subtree include the
// namespaces that subtree was sitting in, so a verifier that canonicalizes the subtree
// alone has to put them back. Redundant declarations are legal XML, and they are the
// difference between verifying Keycloak's assertion and refusing it.
func (t *samlTree) declareInScopeNamespaces(el *etree.Element) {
	for prefix, uri := range t.scope[el] {
		declared := false
		for _, attr := range el.Attr {
			if prefix == "" && attr.Space == "" && attr.Key == "xmlns" {
				declared = true
				break
			}
			if prefix != "" && attr.Space == "xmlns" && attr.Key == prefix {
				declared = true
				break
			}
		}
		if declared || uri == "" {
			continue
		}
		if prefix == "" {
			el.CreateAttr("xmlns", uri)
			continue
		}
		el.CreateAttr("xmlns:"+prefix, uri)
	}
}

// checkSignatureReference is the XML-signature-wrapping check: the signed element
// must be the assertion whose identity we are about to trust, it must carry the
// assertion's own ID, and the signature's reference must point at exactly that ID.
// A signature over a *inner* element while the outer assertion is the one read is
// the classic wrapping attack, and so is a response that contains a signed
// assertion we did not consume.
func checkSignatureReference(t *samlTree, assertion *etree.Element, cfg SAMLConfig) *Refusal {
	signature := t.findSignature(assertion)
	if signature == nil {
		return nil // verifySignature already refused
	}
	assertionID := attrValue(assertion, "ID")
	if strings.TrimSpace(assertionID) == "" {
		return refuse(ReasonSAMLSignature, "the signed assertion carries no ID")
	}
	ref := signature.ChildElements()
	for _, signed := range ref {
		if signed.Tag != "SignedInfo" {
			continue
		}
		for _, reference := range signed.ChildElements() {
			if reference.Tag != "Reference" {
				continue
			}
			uri := strings.TrimPrefix(attrValue(reference, "URI"), "#")
			if uri != assertionID {
				return refuse(ReasonSAMLSignature,
					"the signature covers %q, not the assertion %q being consumed", uri, assertionID)
			}
		}
	}
	return nil
}

// checkSubjectConfirmation requires the bearer method the SAML Web SSO profile
// mandates, with the Recipient of our ACS URL and a live NotOnOrAfter, read from
// the SubjectConfirmationData the assertion carries.
func (t *samlTree) checkSubjectConfirmation(subject *etree.Element, acsURL string, now time.Time) *Refusal {
	confirms := t.children(subject, nsAssertion, "SubjectConfirmation")
	if len(confirms) == 0 {
		return refuse(ReasonSAMLSubject, "the Subject carries no SubjectConfirmation")
	}
	for _, confirm := range confirms {
		if attrValue(confirm, "Method") != "urn:oasis:names:tc:SAML:2.0:cm:bearer" {
			continue
		}
		data := t.child(confirm, nsAssertion, "SubjectConfirmationData")
		if data == nil {
			return refuse(ReasonSAMLConfirmation, "the bearer SubjectConfirmation carries no SubjectConfirmationData")
		}
		if recipient := attrValue(data, "Recipient"); recipient != acsURL {
			return refuse(ReasonSAMLRecipient,
				"the assertion's SubjectConfirmationData is for Recipient %q, not this installation's %q", recipient, acsURL)
		}
		if raw := attrValue(data, "NotOnOrAfter"); raw != "" {
			expiry, err := parseSAMLLocalTime(raw)
			if err != nil {
				return refuse(ReasonSAMLUnparseable, "SubjectConfirmationData NotOnOrAfter is not a SAML timestamp: %v", err)
			}
			if !now.Before(expiry.Add(samlClockSkew)) {
				return refuse(ReasonSAMLNotOnOrAfter, "the SubjectConfirmation expired at %s", raw)
			}
		} else {
			// A bearer confirmation with no expiry is an assertion that never stops
			// being presentable. The profile requires it.
			return refuse(ReasonSAMLConfirmation, "the bearer SubjectConfirmation carries no NotOnOrAfter")
		}
		return nil
	}
	return refuse(ReasonSAMLConfirmation, "the assertion offers no bearer SubjectConfirmation")
}

// checkConditions reads the Audience restriction, the response Recipient, and the
// validity window.
func (t *samlTree) checkConditions(conditions *etree.Element, cfg SAMLConfig, now time.Time) *Refusal {
	if raw := attrValue(conditions, "NotBefore"); raw != "" {
		start, err := parseSAMLLocalTime(raw)
		if err != nil {
			return refuse(ReasonSAMLUnparseable, "Conditions NotBefore is not a SAML timestamp: %v", err)
		}
		if now.Before(start.Add(-samlClockSkew)) {
			return refuse(ReasonSAMLNotValidBefore, "the assertion is not valid before %s", raw)
		}
	}
	if raw := attrValue(conditions, "NotOnOrAfter"); raw != "" {
		expiry, err := parseSAMLLocalTime(raw)
		if err != nil {
			return refuse(ReasonSAMLUnparseable, "Conditions NotOnOrAfter is not a SAML timestamp: %v", err)
		}
		if !now.Before(expiry.Add(samlClockSkew)) {
			return refuse(ReasonSAMLNotOnOrAfter, "the assertion expired at %s", raw)
		}
	}
	for _, restriction := range t.children(conditions, nsAssertion, "AudienceRestriction") {
		names := t.texts(restriction, nsAssertion, "Audience")
		found := false
		for _, name := range names {
			if strings.TrimSpace(name) == cfg.SPEntityID {
				found = true
			}
		}
		if !found {
			return refuse(ReasonSAMLAudience,
				"the assertion's audience is %v; this installation is %q", names, cfg.SPEntityID)
		}
	}
	return nil
}

// identityFromAssertion maps the assertion onto the identity shape the contract
// reads. The NameID is the subject — the stable key the account is filed under —
// and the email is the NameID when the IdP asserted the email format, otherwise the
// configured email attribute.
func (t *samlTree) identity(assertion *etree.Element, cfg SAMLConfig) (*UserInfo, *Refusal) {
	nameID := t.child(assertion, nsAssertion, "Subject", "NameID")
	if nameID == nil {
		return nil, refuse(ReasonSAMLSubject, "the assertion asserts no NameID")
	}
	subject := strings.TrimSpace(nameID.Text())
	format := strings.TrimSpace(attrValue(nameID, "Format"))
	if subject == "" {
		return nil, refuse(ReasonSAMLSubject, "the assertion's NameID is empty")
	}

	attributes := t.attributes(assertion)
	email := ""
	if isEmailNameID(format) && strings.Contains(subject, "@") {
		email = subject
	}
	if email == "" {
		email = firstAttributeValue(attributes, cfg.AttrEmail)
	}
	if email == "" && cfg.NameIDFormat == models.SAMLNameIDFormatEmail && strings.Contains(subject, "@") {
		email = subject
	}
	if email == "" {
		return nil, refuse(ReasonSAMLSubject,
			"the assertion names no email: asserted NameID %q (format %q) and provider %s reads attribute %q",
			subject, format, cfg.ProviderName, cfg.AttrEmail)
	}

	info := &UserInfo{
		Subject: subject,
		Email:   strings.TrimSpace(email),
		Name:    firstAttributeValue(attributes, cfg.AttrName),
		Groups:  attributeValues(attributes, cfg.AttrGroups),
	}
	if username := firstAttributeValue(attributes, cfg.AttrUsername); username != "" {
		info.GivenName = username
	}
	if info.Name == "" {
		info.Name = info.GivenName
	}

	// SAML has no standard "this email is verified" fact. The email-format NameID is
	// the IdP asserting the address itself, which is as verified as this protocol
	// gets; a provider whose IdP sends a verification attribute names it, and
	// `require_verified_email` reads that attribute, not a guess.
	info.EmailVerified = true
	if cfg.VerifiedEmailAttribute != "" {
		info.EmailVerified = boolAttributeValue(attributes, cfg.VerifiedEmailAttribute)
	}
	return info, nil
}

func isEmailNameID(format string) bool {
	return format == "" ||
		strings.HasSuffix(format, ":emailAddress") ||
		strings.EqualFold(format, models.SAMLNameIDFormatEmail)
}

// samlAttribute is one asserted attribute with all its values.
type samlAttribute struct {
	Name  string
	Value []string
}

func (t *samlTree) attributes(assertion *etree.Element) []samlAttribute {
	statement := t.child(assertion, nsAssertion, "AttributeStatement")
	if statement == nil {
		return nil
	}
	var out []samlAttribute
	for _, el := range t.children(statement, nsAssertion, "Attribute") {
		name := strings.TrimSpace(attrValue(el, "Name"))
		if name == "" {
			continue
		}
		values := t.texts(el, nsAssertion, "AttributeValue")
		out = append(out, samlAttribute{Name: name, Value: values})
	}
	return out
}

func attributeValues(attributes []samlAttribute, name string) []string {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute.Name, name) {
			return attribute.Value
		}
	}
	return nil
}

func firstAttributeValue(attributes []samlAttribute, name string) string {
	values := attributeValues(attributes, name)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

// boolAttributeValue reads a boolean attribute. "false" is the only false: an
// attribute that does not exist states nothing, and a provider configured to read
// one must not quietly pass a login because the IdP never sent it.
func boolAttributeValue(attributes []samlAttribute, name string) bool {
	value := strings.ToLower(strings.TrimSpace(firstAttributeValue(attributes, name)))
	if value == "" {
		return false
	}
	return value == "true" || value == "1" || value == "yes"
}

// ReplayStore remembers consumed assertion IDs. The window is derived from the
// expiry the SP accepts, so a captured assertion cannot be re-posted at a moment
// when it would still validate.
type ReplayStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

// NewReplayStore builds a replay store with the default window.
func NewReplayStore() *ReplayStore {
	return &ReplayStore{seen: map[string]time.Time{}, ttl: samlReplayTTL}
}

// Mark consumes an assertion ID. It reports false when the ID was already used:
// the assertion is a replay, and the login is refused.
func (s *ReplayStore) Mark(assertionID string, now time.Time) bool {
	if strings.TrimSpace(assertionID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, seen := range s.seen {
		if now.Sub(seen) > s.ttl {
			delete(s.seen, id)
		}
	}
	if _, used := s.seen[assertionID]; used {
		return false
	}
	s.seen[assertionID] = now
	return true
}

// Seen reports whether an assertion ID has been consumed.
func (s *ReplayStore) Seen(assertionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, used := s.seen[assertionID]
	return used
}

// BuildAuthnRequestID returns a fresh request ID and its issue timestamp. The ID is
// what the response's `InResponseTo` must echo, so it is generated by the SP and
// recorded in the login's server-side state.
func BuildAuthnRequestID() string {
	return "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// SAMLAuthnRequest builds the AuthnRequest document the SP sends.
func SAMLAuthnRequest(cfg SAMLConfig, requestID string, issue time.Time) string {
	nameIDPolicy := ""
	if cfg.NameIDFormat != "" {
		nameIDPolicy = fmt.Sprintf(
			"<samlp:NameIDPolicy Format=%q AllowCreate=\"true\"/>",
			samlNameIDFormatURI(cfg.NameIDFormat))
	}
	return fmt.Sprintf(
		`<samlp:AuthnRequest xmlns:samlp="%s" xmlns:saml="%s" ID="%s" Version="2.0" IssueInstant="%s" Destination="%s" ProtocolBinding="%s" AssertionConsumerServiceURL="%s">`+
			`<saml:Issuer>%s</saml:Issuer>%s</samlp:AuthnRequest>`,
		// `Destination` names the *recipient* — the IdP's SSO location the request is
		// addressed to — while `AssertionConsumerServiceURL` names where the assertion
		// must come back to. A conformant IdP checks the first against its own endpoint
		// and refuses the request when it names something else, which is exactly what
		// Keycloak did to us (`invalid_destination`): the ACS is the answer's address,
		// not the question's.
		nsProtocol, nsAssertion, requestID, issue.UTC().Format(samlTimeLayout), xmlEscape(cfg.IdpSSOURL),
		bindingPOST, xmlEscape(cfg.ACSURL), xmlEscape(cfg.SPEntityID), nameIDPolicy)
}

// SPInitiatedRedirect deflates, base64-encodes and (when the provider has an SP key)
// signs an AuthnRequest, and returns the IdP URL to send the browser to.
//
// The signed bytes are the **transmitted** query string, not the parameter values
// before encoding. A conformant IdP rebuilds the signing string from the octets of the
// URL it received and verifies against those, so signing the pre-escape base64 yields a
// request a real IdP refuses: base64 carries `+`, `/` and `=`, and it is the escaped
// form that crosses the wire. (Found the hard way against Keycloak, which answers
// `invalid_signature`; OurWay's own peer-IdP fixture never notices, because it reads
// the decoded parameters.)
func SPInitiatedRedirect(requestXML string, cfg SAMLConfig) (redirect string, err error) {
	encoded, err := DeflateSAMLRequest(requestXML)
	if err != nil {
		return "", err
	}
	base := cfg.IdpSSOURL
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	// The request, the relay state and the algorithm, escaped exactly as they are
	// transmitted: this string is the prefix of the URL the browser is sent to, which
	// is the point of building it here rather than signing the decoded parameters.
	query := buildRedirectToSign(encoded, cfg.SPEntityID, sigAlgRSA_SHA256)
	if !cfg.SignRequests {
		return base + separator + query, nil
	}
	key, keyErr := parsePEMPrivateKey(cfg.SPKeyPEM)
	if keyErr != nil {
		return "", fmt.Errorf("provider %s requires signed requests: %v", cfg.ProviderName, keyErr)
	}
	// A signed request names its algorithm: a receiver that found a Signature and no
	// SigAlg has nothing to verify it with, and guessing is not a rule.
	signature, signErr := signRSASHA256(key, []byte(query))
	if signErr != nil {
		return "", fmt.Errorf("provider %s requires signed requests: %v", cfg.ProviderName, signErr)
	}
	// The signature is appended to the string it covers, so the URL the browser is
	// sent to begins with exactly the bytes that were signed.
	return base + separator + query + "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(signature)), nil
}

// buildRedirectToSign is the HTTP-Redirect signing string the SAML profile defines:
// the deflated-and-encoded request and the relay state, percent-encoded as they are
// transmitted, concatenated in that order with no separators. Appending the algorithm
// (and then the signature) gives the whole redirect query, which is why the signed
// string and the transmitted string cannot drift apart.
func buildRedirectToSign(encodedRequest, relayState, alg string) string {
	query := "SAMLRequest=" + url.QueryEscape(encodedRequest) + "&RelayState=" + url.QueryEscape(relayState)
	if alg == "" {
		return query
	}
	return query + "&SigAlg=" + url.QueryEscape(alg)
}

// DeflateSAMLRequest deflates and base64-encodes a request, as the HTTP-Redirect
// binding requires (raw deflate, no zlib header).
func DeflateSAMLRequest(requestXML string) (string, error) {
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return "", err
	}
	if _, err := writer.Write([]byte(requestXML)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// InflateSAMLRequest reverses DeflateSAMLRequest, for an IdP that posts a
// request to us (IdP-initiated SSO) and for the round-trip test.
func InflateSAMLRequest(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	if len(raw) > maxAssertionBytes {
		return "", fmt.Errorf("SAMLRequest is %d bytes, over the %d byte cap", len(raw), maxAssertionBytes)
	}
	reader := flate.NewReader(bytes.NewReader(raw))
	defer reader.Close()
	out, err := io.ReadAll(io.LimitReader(reader, maxAssertionBytes))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SPMetadata renders the SP metadata document an operator uploads to the IdP. It
// carries the entity ID, the ACS location and binding, the NameID format we ask for,
// the signature and digest algorithms we accept, and the SP signing certificate when
// one is configured. It carries no private key, and it never does.
func SPMetadata(cfg SAMLConfig) (string, error) {
	if strings.TrimSpace(cfg.SPEntityID) == "" {
		return "", errors.New("provider has no SP entity id")
	}
	if strings.TrimSpace(cfg.ACSURL) == "" {
		return "", errors.New("provider has no assertion consumer service URL")
	}
	signing := ""
	if cert, err := parsePEMCertificate(cfg.SPCertPEM); err == nil && cert != nil {
		signing = fmt.Sprintf(
			`<ds:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>%s</ds:X509Certificate></ds:X509Data></ds:KeyInfo></ds:KeyDescriptor>`,
			base64.StdEncoding.EncodeToString(cert.Raw))
	}
	doc := fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<EntityDescriptor xmlns="%s" xmlns:saml="%s" xmlns:ds="%s" entityID="%s">`+
			`<SPSSODescriptor AuthnRequestsSigned="%t" WantAssertionsSigned="true" protocolSupportEnumeration="%s">`+
			`%s`+
			`<NameIDFormat>%s</NameIDFormat>`+
			`<AssertionConsumerService Binding="%s" Location="%s" index="0" isDefault="true"/>`+
			`</SPSSODescriptor>`+
			`</EntityDescriptor>`,
		nsMetadata, nsAssertion, nsDS, xmlEscape(cfg.SPEntityID),
		cfg.SignRequests, nsProtocol, signing,
		samlNameIDFormatURI(cfg.NameIDFormat),
		bindingPOST, xmlEscape(cfg.ACSURL))
	// Round-trip the document: metadata an IdP cannot parse is a configuration
	// failure the operator discovers after a day of not being able to log in.
	probe := etree.NewDocument()
	if err := probe.ReadFromString(doc); err != nil {
		return "", fmt.Errorf("generated metadata does not parse: %w", err)
	}
	return doc, nil
}

// SPFor derives the SP identity for a provider row. The entity ID names this
// installation and this provider, so two installations pointing at the same IdP are
// two entities, and the assertion's audience says which one it was issued for.
func SPFor(provider *models.SSOProvider) string {
	return fmt.Sprintf("urn:ourway:%s:%s", strings.TrimSpace(provider.Name), provider.ID)
}

// CertificateFingerprint is the SHA-256 fingerprint of the provider's pinned IdP
// certificate, in the colon-separated form certificate tooling prints. The console
// shows this instead of the certificate, and it is the value an operator compares
// with what the IdP published.
func CertificateFingerprint(pemText string) string {
	cert, err := parsePEMCertificate(pemText)
	if err != nil || cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.Raw)
	parts := make([]string, 0, len(sum))
	for _, b := range sum {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, ":")
}

// ValidateCertificatePEM reports the operator-facing problem with a certificate the
// console typed, including the one that matters most: a certificate whose validity
// window has already closed, which is a provider that will start refusing logins
// (or has already started) for a reason nothing in the UI states.
func ValidateCertificatePEM(pemText string, now time.Time) string {
	cert, err := parsePEMCertificate(pemText)
	if err != nil {
		return err.Error()
	}
	if cert.NotAfter.Before(now) {
		return fmt.Sprintf("certificate expired at %s", cert.NotAfter.UTC().Format(time.RFC3339))
	}
	return ""
}

// GenerateSPKeyPair builds a signing key pair for a provider that needs signed
// requests: 3072-bit RSA, self-signed certificate valid for a year, PEM-encoded.
// OurWay generates it because the alternative is the operator shelling out to
// openssl, and the key never leaves the installation.
func GenerateSPKeyPair(entityID string, now time.Time) (certPEM string, keyPEM string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	template.Subject.CommonName = entityID
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return certPEM, keyPEM, nil
}

// signRSASHA256 signs bytes with the SP key, the algorithm the HTTP-Redirect
// binding's `SigAlg` names.
func signRSASHA256(key *rsa.PrivateKey, payload []byte) ([]byte, error) {
	sum := sha256.Sum256(payload)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
}

// parsePEMCertificate reads a PEM certificate, accepting the armored form an IdP
// publishes and the bare base64 DER an operator pastes from a console.
func parsePEMCertificate(text string) (*x509.Certificate, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, errors.New("no certificate")
	}
	if strings.HasPrefix(trimmed, "-----BEGIN") {
		block, _ := pem.Decode([]byte(trimmed))
		if block == nil {
			return nil, errors.New("certificate is not valid PEM")
		}
		return x509.ParseCertificate(block.Bytes)
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(trimmed), ""))
	if err != nil {
		return nil, errors.New("certificate is neither PEM nor base64 DER")
	}
	return x509.ParseCertificate(der)
}

func parsePEMPrivateKey(text string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(text)))
	if block == nil {
		return nil, errors.New("the SP key is not valid PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the SP key is not a PKCS#1 or PKCS#8 key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the SP key is a %T, not RSA", parsed)
	}
	return key, nil
}

// parseSAMLLocalTime parses an xs:dateTime as SAML requires it: UTC, second
// precision, no offset. A timestamp with an offset is not what the profile permits,
// and a parser that accepted one would silently widen every expiry window.
func parseSAMLLocalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	parsed, err := time.Parse(samlTimeLayout, raw)
	if err == nil {
		return parsed.UTC(), nil
	}
	parsed, err = time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("unparsable SAML timestamp %q", raw)
	}
	if zone := raw[len(raw)-1]; zone != 'Z' && zone != 'z' {
		return time.Time{}, fmt.Errorf("SAML timestamp %q must be UTC", raw)
	}
	return parsed.UTC(), nil
}

func samlNameIDFormatURI(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "persistent":
		return "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	case "transient":
		return "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
	case "unspecified":
		return "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified"
	default:
		return nameIDFormatEmail
	}
}

// tagOf names an element for a message, defensively (a nil element is a real
// possibility when a document is short).
func tagOf(el *etree.Element) string {
	if el == nil {
		return "(none)"
	}
	return el.Tag
}

// samlTree resolves the namespace a parsed element is written *in*. SAML documents
// bind namespaces to prefixes (`<saml:Issuer>` with `xmlns:saml="urn:oasis:..."`),
// and the prefix is not the namespace: an IdP that writes the assertion in the
// default namespace, with no prefix at all, asserts the same thing. Matching on the
// prefix would accept the documents one IdP writes and refuse the documents another
// writes, with the refusals reading as "no assertion". So every check below matches
// on the resolved URI, and an element whose prefix resolves to nothing matches no
// namespace at all.
type samlTree struct {
	scope map[*etree.Element]map[string]string
}

func newSamlTree(root *etree.Element) *samlTree {
	tree := &samlTree{scope: map[*etree.Element]map[string]string{}}
	var walk func(el *etree.Element, inherited map[string]string)
	walk = func(el *etree.Element, inherited map[string]string) {
		scope := make(map[string]string, len(inherited)+1)
		for prefix, uri := range inherited {
			scope[prefix] = uri
		}
		for _, attr := range el.Attr {
			if attr.Space == "xmlns" && attr.Key != "" {
				scope[attr.Key] = attr.Value
			} else if attr.Space == "" && attr.Key == "xmlns" {
				scope[""] = attr.Value
			}
		}
		tree.scope[el] = scope
		for _, child := range el.ChildElements() {
			walk(child, scope)
		}
	}
	if root != nil {
		walk(root, map[string]string{})
	}
	return tree
}

// namespaceOf is the namespace URI an element is written in.
func (t *samlTree) namespaceOf(el *etree.Element) string {
	if el == nil {
		return ""
	}
	return t.scope[el][el.Space]
}

// child returns el's first child in `uri` with the local name `tag`, descending
// through `rest`.
func (t *samlTree) child(el *etree.Element, uri, tag string, rest ...string) *etree.Element {
	if el == nil {
		return nil
	}
	for _, next := range el.ChildElements() {
		if next.Tag != tag || t.namespaceOf(next) != uri {
			continue
		}
		if len(rest) == 0 {
			return next
		}
		return t.child(next, uri, rest[0], rest[1:]...)
	}
	return nil
}

func (t *samlTree) children(el *etree.Element, uri, tag string) []*etree.Element {
	var out []*etree.Element
	if el == nil {
		return out
	}
	for _, next := range el.ChildElements() {
		if next.Tag == tag && t.namespaceOf(next) == uri {
			out = append(out, next)
		}
	}
	return out
}

func (t *samlTree) text(el *etree.Element, uri, tag string, rest ...string) string {
	target := t.child(el, uri, tag, rest...)
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Text())
}

func (t *samlTree) texts(el *etree.Element, uri, tag string) []string {
	var out []string
	for _, child := range t.children(el, uri, tag) {
		out = append(out, strings.TrimSpace(child.Text()))
	}
	return out
}

func xmlEscape(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return ""
	}
	return buf.String()
}

// attrValue reads an element attribute, answering "" when it is absent. etree hands
// back the attribute struct; the SAML checks all want its value.
func attrValue(el *etree.Element, name string) string {
	if el == nil {
		return ""
	}
	return el.SelectAttrValue(name, "")
}
