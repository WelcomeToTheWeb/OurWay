// Package testsaml is a SAML 2.0 Identity Provider fixture for the test suite
// (2.4-7).
//
// It is an implementation of the protocol, not a mock of OurWay's SAML layer: it
// holds a signing key, mints assertions, signs them with real XML-DSig
// (enveloped RSA-SHA256 over the canonicalized assertion), assembles the Response
// the IdP posts to an ACS, and sabotages itself on command.
//
// The reason is the same one that put `testidp` in the OIDC tests: a stub of the
// verifier agrees with whatever the verifier does. The assertions 2.4-7's
// acceptance criteria name — tampered, expired, wrong audience, wrong recipient,
// replayed, unsigned — only mean something when a *signer* produced the document
// first and the fixture broke it afterwards.
package testsaml

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/google/uuid"
	dsig "github.com/russellhaering/goxmldsig"
)

// Namespace constants, exported so a test can read a document the fixture produced
// without repeating the URNs.
const (
	Namespace     = "urn:oasis:names:tc:SAML:2.0:assertion"
	ProtoNS       = "urn:oasis:names:tc:SAML:2.0:protocol"
	NameIDEmail   = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
	ConfirmBear   = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
	StatusSuccess = "urn:oasis:names:tc:SAML:2.0:status:Success"
	BindingPOST   = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
)

// IdP is a running SAML identity provider fixture: a signing key, an entity ID, and
// the ability to mint assertions about anyone.
type IdP struct {
	Key      *rsa.PrivateKey
	Cert     *x509.Certificate
	EntityID string
	// SSOURL and LogoutURL are the locations the IdP publishes in its metadata;
	// the SP redirects to SSOURL.
	SSOURL    string
	LogoutURL string

	now func() time.Time
}

// New starts a fixture with a freshly generated RSA signing key and a self-signed
// certificate valid for a year — the state an operator receives from an IdP console.
func New() *IdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "testsaml-idp"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	return &IdP{
		Key:       key,
		Cert:      cert,
		EntityID:  "https://idp.testsaml.example/" + id,
		SSOURL:    "https://idp.testsaml.example/sso/" + id,
		LogoutURL: "https://idp.testsaml.example/logout/" + id,
		now:       time.Now,
	}
}

// SetClock replaces the fixture's clock, so a test can mint an assertion whose
// expiry is in the past without waiting for it to expire.
func (p *IdP) SetClock(clock func() time.Time) { p.now = clock }

// CertificatePEM is the signing certificate in the armored form an IdP console
// publishes. This is what the operator pastes into OurWay.
func (p *IdP) CertificatePEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.Cert.Raw}))
}

// Claims is what the IdP asserts about a person for one login.
type Claims struct {
	NameID          string
	NameIDFormat    string
	Email           string
	Username        string
	DisplayName     string
	Groups          []string
	GroupsAttribute string
	// VerifiedAttribute names the boolean attribute that states the email is
	// verified; empty means the IdP sends no such attribute.
	VerifiedAttribute string
	Verified          bool
	// ExtraAttributes is a raw attribute list: {name, value1, value2, ...}.
	ExtraAttributes [][]string
}

// AssertionOptions is the shape of one assertion the fixture mints. Defaults are
// what a conformant IdP produces; every field is how a negative fixture deviates.
type AssertionOptions struct {
	Audience            string
	AudienceExtra       []string
	Recipient           string
	Destination         string
	Issuer              string
	NotOnOrAfter        time.Time
	NotBefore           time.Time
	AuthnInstant        time.Time
	SessionNotOnOrAfter time.Time
	// NoSignature mints the assertion unsigned.
	NoSignature bool
	// NoSubjectConfirmation, NoConfirmationData, NoConfirmationExpiry and
	// ConfirmationMethod state the SubjectConfirmation shapes the profile forbids.
	NoSubjectConfirmation bool
	NoConfirmationData    bool
	NoConfirmationExpiry  bool
	ConfirmationMethod    string
	// NoNameID mints an assertion that asserts no NameID at all.
	NoNameID bool
	// NameIDFormatOverride is written verbatim into the NameID's Format attribute.
	NameIDFormatOverride string
	// Conditions, when false, omits the Conditions element.
	Conditions *bool
	// AssertionID is the assertion's own ID; empty generates one.
	AssertionID string
	// SignedElementID signs an element carrying *this* ID while the assertion the
	// Response carries keeps another: the wrapping fixture, where a signature
	// verifies over an element that is not the one being consumed.
	SignedElementID string
	// SignResponse makes the fixture sign the enclosing Response too.
	SignResponse bool
	// ResponseIssuer writes a *different* Issuer into the Response element: the
	// fixture where the enclosing document names the configured IdP while a nested
	// assertion was minted by another key.
	ResponseIssuer string
	// ResponseDestination writes a *different* Destination into the Response
	// element than the ACS the assertion is addressed to: the fixture where the
	// document posted to us is addressed to someone else.
	ResponseDestination string
}

// AssertionXML mints a signed assertion and returns its XML.
func (p *IdP) AssertionXML(claims Claims, opts AssertionOptions) string {
	now := p.now()
	assertionID := strings.TrimSpace(opts.AssertionID)
	if assertionID == "" {
		assertionID = "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	notOnOrAfter := opts.NotOnOrAfter
	if notOnOrAfter.IsZero() {
		notOnOrAfter = now.Add(5 * time.Minute)
	}
	authnInstant := opts.AuthnInstant
	if authnInstant.IsZero() {
		authnInstant = now
	}
	sessionNotOnOrAfter := opts.SessionNotOnOrAfter
	if sessionNotOnOrAfter.IsZero() {
		sessionNotOnOrAfter = notOnOrAfter
	}
	issuer := strings.TrimSpace(opts.Issuer)
	if issuer == "" {
		issuer = p.EntityID
	}
	confirmationMethod := strings.TrimSpace(opts.ConfirmationMethod)
	if confirmationMethod == "" {
		confirmationMethod = ConfirmBear
	}

	doc := etree.NewDocument()
	assertion := doc.CreateElement("Assertion")
	assertion.Space = "saml"
	assertion.CreateAttr("xmlns:saml", Namespace)
	assertion.CreateAttr("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance")
	assertion.CreateAttr("ID", assertionID)
	assertion.CreateAttr("Version", "2.0")
	assertion.CreateAttr("IssueInstant", utc(authnInstant))
	assertion.CreateAttr("NotOnOrAfter", utc(notOnOrAfter))

	el(assertion, "saml", "Issuer").SetText(issuer)

	subject := el(assertion, "saml", "Subject")
	if !opts.NoNameID {
		nameID := el(subject, "saml", "NameID")
		format := claims.NameIDFormat
		if opts.NameIDFormatOverride != "" {
			format = opts.NameIDFormatOverride
		}
		if format != "" {
			nameID.CreateAttr("Format", format)
		}
		nameID.SetText(claims.NameID)
	}
	if !opts.NoSubjectConfirmation {
		confirmation := el(subject, "saml", "SubjectConfirmation")
		confirmation.CreateAttr("Method", confirmationMethod)
		if !opts.NoConfirmationData {
			data := el(confirmation, "saml", "SubjectConfirmationData")
			if opts.Recipient != "" {
				data.CreateAttr("Recipient", opts.Recipient)
			}
			if !opts.NoConfirmationExpiry {
				data.CreateAttr("NotOnOrAfter", utc(notOnOrAfter))
			}
			if opts.Destination != "" {
				data.CreateAttr("Address", opts.Destination)
			}
		}
	}

	if opts.Conditions == nil || *opts.Conditions {
		conditions := el(assertion, "saml", "Conditions")
		if !opts.NotBefore.IsZero() {
			conditions.CreateAttr("NotBefore", utc(opts.NotBefore))
		}
		conditions.CreateAttr("NotOnOrAfter", utc(notOnOrAfter))
		if opts.Audience != "" || len(opts.AudienceExtra) > 0 {
			restriction := el(conditions, "saml", "AudienceRestriction")
			for _, audience := range append([]string{opts.Audience}, opts.AudienceExtra...) {
				if strings.TrimSpace(audience) == "" {
					continue
				}
				el(restriction, "saml", "Audience").SetText(audience)
			}
		}
	}

	statement := el(assertion, "saml", "AuthnStatement")
	statement.CreateAttr("AuthnInstant", utc(authnInstant))
	statement.CreateAttr("SessionNotOnOrAfter", utc(sessionNotOnOrAfter))
	el(statement, "saml", "SessionIndex").SetText("_session_" + strings.ReplaceAll(uuid.NewString(), "-", ""))

	if attributes := attributeList(claims); len(attributes) > 0 {
		list := el(assertion, "saml", "AttributeStatement")
		for _, attribute := range attributes {
			attrib := el(list, "saml", "Attribute")
			attrib.CreateAttr("Name", attribute[0])
			attrib.CreateAttr("NameFormat", "urn:oasis:names:tc:SAML:2.0:attrname-format:basic")
			for _, value := range attribute[1:] {
				el(attrib, "saml", "AttributeValue").SetText(value)
			}
		}
	}

	if !opts.NoSignature {
		toSign := assertion
		if opts.SignedElementID != "" {
			// The Reference URI is built from the ID attribute of the element handed
			// to the signing context: signing a copy that carries the *other* ID is
			// how the wrapping fixture is produced.
			toSign = assertion.Copy()
			for i, attr := range toSign.Attr {
				if attr.Key == "ID" {
					toSign.Attr[i].Value = opts.SignedElementID
				}
			}
		}
		signature, err := p.sign(toSign, true)
		if err != nil {
			panic("testsaml: " + err.Error())
		}
		// The SAML schema puts the Signature immediately after the Issuer.
		insertAfterIssuer(assertion, signature)
	}

	xml, err := doc.WriteToString()
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	return xml
}

func attributeList(claims Claims) [][]string {
	var out [][]string
	if claims.Email != "" {
		out = append(out, []string{"email", claims.Email})
	}
	if claims.Username != "" {
		out = append(out, []string{"username", claims.Username})
	}
	if claims.DisplayName != "" {
		out = append(out, []string{"displayName", claims.DisplayName})
	}
	if len(claims.Groups) > 0 {
		name := claims.GroupsAttribute
		if name == "" {
			name = "groups"
		}
		out = append(out, append([]string{name}, claims.Groups...))
	}
	if claims.VerifiedAttribute != "" {
		value := "false"
		if claims.Verified {
			value = "true"
		}
		out = append(out, []string{claims.VerifiedAttribute, value})
	}
	return append(out, claims.ExtraAttributes...)
}

// ResponseXML assembles the Response the IdP posts to an ACS. `inResponseTo` is the
// AuthnRequest ID it answers and `destination` is the ACS URL: the two facts that
// tie an assertion to a login the SP started.
func (p *IdP) ResponseXML(claims Claims, opts AssertionOptions, acsURL, inResponseTo, spEntityID string) string {
	assertionOpts := opts
	if assertionOpts.Audience == "" {
		assertionOpts.Audience = spEntityID
	}
	if assertionOpts.Recipient == "" {
		assertionOpts.Recipient = acsURL
	}
	if assertionOpts.Destination == "" {
		assertionOpts.Destination = acsURL
	}
	assertion := p.AssertionXML(claims, assertionOpts)

	doc := etree.NewDocument()
	response := doc.CreateElement("Response")
	response.Space = "samlp"
	response.CreateAttr("xmlns:samlp", ProtoNS)
	response.CreateAttr("xmlns:saml", Namespace)
	response.CreateAttr("ID", "_resp_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	response.CreateAttr("Version", "2.0")
	response.CreateAttr("IssueInstant", utc(p.now()))
	destination := acsURL
	if opts.ResponseDestination != "" {
		destination = opts.ResponseDestination
	}
	response.CreateAttr("Destination", destination)
	if inResponseTo != "" {
		response.CreateAttr("InResponseTo", inResponseTo)
	}

	responseIssuer := p.EntityID
	if opts.ResponseIssuer != "" {
		responseIssuer = opts.ResponseIssuer
	}
	el(response, "saml", "Issuer").SetText(responseIssuer)
	status := el(response, "samlp", "Status")
	el(status, "samlp", "StatusCode").CreateAttr("Value", StatusSuccess)

	probe := etree.NewDocument()
	if err := probe.ReadFromString(assertion); err != nil {
		panic("testsaml: " + err.Error())
	}
	response.AddChild(probe.Root())

	if opts.SignResponse {
		// Sign the *serialized* document, like an IdP does: the bytes that will be
		// posted are the bytes the signature covers. Signing an in-memory tree that
		// was assembled from another document's elements signs a canonical form the
		// posted document does not reproduce.
		xml, err := doc.WriteToString()
		if err != nil {
			panic("testsaml: " + err.Error())
		}
		fresh := etree.NewDocument()
		if err := fresh.ReadFromString(xml); err != nil {
			panic("testsaml: " + err.Error())
		}
		response = fresh.Root()
		signature, err := p.sign(response, true)
		if err != nil {
			panic("testsaml: " + err.Error())
		}
		insertAfterIssuer(response, signature)
		xml, err = fresh.WriteToString()
		if err != nil {
			panic("testsaml: " + err.Error())
		}
		return xml
	}

	xml, err := doc.WriteToString()
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	return xml
}

// ResponseFormValue is the `SAMLResponse` form field the browser posts to an ACS.
func (p *IdP) ResponseFormValue(claims Claims, opts AssertionOptions, acsURL, inResponseTo, spEntityID string) string {
	return base64.StdEncoding.EncodeToString([]byte(p.ResponseXML(claims, opts, acsURL, inResponseTo, spEntityID)))
}

// StatusResponse mints a Response carrying a non-Success status: the case where the
// IdP itself refused the login.
func (p *IdP) StatusResponse(acsURL, inResponseTo, statusCode, message string) string {
	doc := etree.NewDocument()
	response := doc.CreateElement("Response")
	response.Space = "samlp"
	response.CreateAttr("xmlns:samlp", ProtoNS)
	response.CreateAttr("xmlns:saml", Namespace)
	response.CreateAttr("ID", "_resp_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	response.CreateAttr("Version", "2.0")
	response.CreateAttr("IssueInstant", utc(p.now()))
	response.CreateAttr("Destination", acsURL)
	if inResponseTo != "" {
		response.CreateAttr("InResponseTo", inResponseTo)
	}
	el(response, "saml", "Issuer").SetText(p.EntityID)
	status := el(response, "samlp", "Status")
	el(status, "samlp", "StatusCode").CreateAttr("Value", statusCode)
	if message != "" {
		el(status, "samlp", "StatusMessage").SetText(message)
	}
	xml, err := doc.WriteToString()
	if err != nil {
		panic("testsaml: " + err.Error())
	}
	return xml
}

// sign produces the enveloped RSA-SHA256 XML signature over one element, using the
// exclusive, omit-comments canonicalization the SAML SSO profile requires — the
// canonicalization a real IdP's signature declares, so the fixture's signatures are
// the shape the verifier meets in production, not a shape only this library makes.
func (p *IdP) sign(toSign *etree.Element, enveloped bool) (*etree.Element, error) {
	ctx, err := dsig.NewSigningContext(p.Key, [][]byte{p.Cert.Raw})
	if err != nil {
		return nil, err
	}
	ctx.Hash = crypto.SHA256
	ctx.IdAttribute = "ID"
	ctx.Prefix = "ds"
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		return nil, err
	}
	return ctx.ConstructSignature(toSign, enveloped)
}

// el creates a child element in the given prefix's namespace.
func el(parent *etree.Element, prefix, tag string) *etree.Element {
	child := parent.CreateElement(tag)
	child.Space = prefix
	return child
}

// insertAfterIssuer puts a Signature where the SAML schema puts it: immediately
// after the Issuer, or first when there is none.
func insertAfterIssuer(host *etree.Element, signature *etree.Element) {
	position := 0
	for i, child := range host.Child {
		if element, ok := child.(*etree.Element); ok && element.Tag == "Issuer" {
			position = i + 1
			break
		}
	}
	host.InsertChildAt(position, signature)
}

func utc(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
