package api

// 2.4-7 · SAML 2.0, tested through the routes.
//
// The tests walk the real flow: the SP's `authorize` redirect, the metadata the
// operator uploads to the IdP, the assertion the browser POSTs to the assertion
// consumer service, and the session the console is issued from it. The fixture is the
// protocol implementation from `sso/testsaml` — it signs with real XML-DSig — so the
// refusals below are produced by breaking signed documents, and the acceptance is
// produced by the document a conformant IdP sends.
//
// What the file is particular about: 2.4-3's identity contract must be reached
// unchanged (an SSO identity claims the account it belongs to, never the account its
// email names), 2.4-2's rule that no token travels in a URL must survive the POST
// that carries an assertion, and every refusal must be a named reason in the ledger.

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"ourway/server/models"
	"ourway/server/sso"
	"ourway/server/sso/testidp"
	"ourway/server/sso/testsaml"
)

// samlProviderForm is the SAML half of the provider form: the facts the IdP console
// gives the operator. The endpoints are not typed here — a SAML provider has an
// entity ID and an SSO location, and the destination is derived by OurWay.
func samlProviderForm(name string, idp *testsaml.IdP, extra map[string]any) map[string]any {
	form := map[string]any{
		"type":            models.SSOTypeSAML,
		"name":            name,
		"idp_entity_id":   idp.EntityID,
		"idp_sso_url":     idp.SSOURL,
		"idp_logout_url":  idp.LogoutURL,
		"idp_certificate": idp.CertificatePEM(),
		"nameid_format":   models.SAMLNameIDFormatEmail,
		"generate_sp_key": true,
		"link_policy":     models.SSOLinkPolicyPending,
	}
	for key, value := range extra {
		form[key] = value
	}
	return form
}

// saveSAMLProvider saves a SAML provider and returns its row id.
func (f *ssoFixture) saveSAMLProvider(t *testing.T, admin, name string, idp *testsaml.IdP, extra map[string]any) string {
	t.Helper()
	code, body := f.saveProvider(t, admin, samlProviderForm(name, idp, extra))
	if code != http.StatusOK {
		t.Fatalf("save saml provider = %d\n%s", code, body)
	}
	var answer struct {
		Provider models.SSOProvider `json:"provider"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	return answer.Provider.ID
}

// samlSP is the installation's side of a provider: the entity ID the assertion's
// audience must name and the ACS URL it must be addressed to, read from the SP
// metadata the operator uploads to the IdP. Every round trip below is built from
// these, so a provider whose metadata and validation disagree fails here.
type samlSP struct {
	EntityID string
	ACSURL   string
	Metadata string
}

func (f *ssoFixture) samlMetadata(t *testing.T, name string) (int, []byte, http.Header) {
	t.Helper()
	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/saml/" + name + "/metadata")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	return resp.StatusCode, body, resp.Header
}

func (f *ssoFixture) samlSP(t *testing.T, name string) samlSP {
	t.Helper()
	status, body, header := f.samlMetadata(t, name)
	if status != http.StatusOK {
		t.Fatalf("metadata = %d\n%s", status, body)
	}
	if ctype := header.Get("Content-Type"); ctype != "application/samlmetadata+xml" {
		t.Fatalf("metadata content type is %q", ctype)
	}
	var descriptor struct {
		EntityID   string `xml:"entityID,attr"`
		Descriptor struct {
			ACS struct {
				Location string `xml:"Location,attr"`
			} `xml:"AssertionConsumerService"`
		} `xml:"SPSSODescriptor"`
	}
	if err := xml.Unmarshal(body, &descriptor); err != nil {
		t.Fatalf("metadata does not unmarshal: %v", err)
	}
	if descriptor.EntityID == "" || descriptor.Descriptor.ACS.Location == "" {
		t.Fatalf("metadata names no entity id or ACS:\n%s", body)
	}
	return samlSP{
		EntityID: descriptor.EntityID,
		ACSURL:   descriptor.Descriptor.ACS.Location,
		Metadata: string(body),
	}
}

// startSAMLLogin walks the SP-initiated step: the authorize redirect, the deflated
// AuthnRequest the IdP receives, and the request ID the response must echo.
func (f *ssoFixture) startSAMLLogin(t *testing.T, providerName string) string {
	t.Helper()
	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/" + providerName + "/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize = %d\n%s", resp.StatusCode, body)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize location: %v", err)
	}
	encoded := location.Query().Get("SAMLRequest")
	if encoded == "" {
		t.Fatalf("the authorize redirect carries no SAMLRequest: %s", location)
	}
	requestXML, err := sso.InflateSAMLRequest(encoded)
	if err != nil {
		t.Fatalf("the SAMLRequest does not inflate: %v", err)
	}
	var request struct {
		ID          string `xml:"ID,attr"`
		Destination string `xml:"Destination,attr"`
		Issuer      string `xml:"Issuer"`
	}
	if err := xml.Unmarshal([]byte(requestXML), &request); err != nil {
		t.Fatalf("the AuthnRequest does not parse: %v\n%s", err, requestXML)
	}
	if request.ID == "" {
		t.Fatal("the AuthnRequest carries no ID for the response to answer")
	}
	return request.ID
}

// postSAMLAssertion posts a SAMLResponse form to a provider's ACS and reports what
// came back. `code` is the one-time login code when the redirect landed on the login
// page, `reason` the named refusal otherwise.
func (f *ssoFixture) postSAMLAssertion(t *testing.T, providerName, formValue string) (status int, code, reason, location string) {
	t.Helper()
	form := url.Values{}
	form.Set("SAMLResponse", formValue)
	resp, err := f.noRedirect().Post(f.baseURL+"/api/auth/sso/saml/"+providerName+"/acs",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("acs post: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read acs answer: %v", err)
	}
	location = resp.Header.Get("Location")
	if resp.StatusCode == http.StatusFound {
		if strings.Contains(location, "access_token") || strings.Contains(location, "refresh_token") {
			t.Fatalf("2.4-2's rule: a session is never issued from a URL; the redirect is %s", location)
		}
		loginCode := resp.Header.Get("Location")
		if idx := strings.Index(loginCode, "sso_code="); idx >= 0 {
			code = loginCode[idx+len("sso_code="):]
		}
		if code == "" {
			t.Fatalf("the ACS redirect carries no login code: %s", location)
		}
		return resp.StatusCode, code, "", location
	}
	var answer struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(body, &answer)
	return resp.StatusCode, "", answer.Reason, location
}

// redeemSAMLSession exchanges the login code for the session, the same door the OIDC
// flow uses: the tokens travel in a POST body, never in a URL.
func (f *ssoFixture) redeemSAMLSession(t *testing.T, code string) string {
	t.Helper()
	status, body, _ := f.do(t, http.MethodPost, "/api/auth/sso/exchange", "", map[string]any{"code": code})
	if status != http.StatusOK {
		t.Fatalf("exchange = %d\n%s", status, body)
	}
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode exchange: %v\n%s", err, body)
	}
	if answer.AccessToken == "" {
		t.Fatalf("the exchange issued no access token: %s", body)
	}
	return answer.AccessToken
}

// samlLogin walks the whole flow and returns the access token, failing on any refusal.
func (f *ssoFixture) samlLogin(t *testing.T, providerName string, sp samlSP, idp *testsaml.IdP,
	claims testsaml.Claims, opts testsaml.AssertionOptions) string {
	t.Helper()
	requestID := f.startSAMLLogin(t, providerName)
	formValue := idp.ResponseFormValue(claims, opts, sp.ACSURL, requestID, sp.EntityID)
	status, code, reason, location := f.postSAMLAssertion(t, providerName, formValue)
	if status != http.StatusFound {
		t.Fatalf("acs post = %d (reason %q, location %q)", status, reason, location)
	}
	return f.redeemSAMLSession(t, code)
}

// profileOf reads a session's own account: the fixture's window on roles, because
// every SSO assertion here is about *which account and which roles* an identity
// became.
func (f *ssoFixture) profileOf(t *testing.T, access string) (models.User, []string) {
	t.Helper()
	status, body, _ := f.do(t, http.MethodGet, "/api/auth/profile", access, nil)
	if status != http.StatusOK {
		t.Fatalf("profile = %d\n%s", status, body)
	}
	var answer struct {
		User  models.User `json:"user"`
		Roles []string    `json:"roles"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode profile: %v\n%s", err, body)
	}
	return answer.User, answer.Roles
}

// samlClaims is the person the fixture IdP asserts: NameID is her email address, in
// the format the provider asked for.
func samlClaims() testsaml.Claims {
	return testsaml.Claims{
		NameID:       "ada@corp.example",
		NameIDFormat: testsaml.NameIDEmail,
		Email:        "ada@corp.example",
		Username:     "ada",
		DisplayName:  "Ada Lovelace",
		Groups:       []string{"rmm-operators"},
	}
}

// TestSAMLProviderIsBuiltFromTheIdPFacts is the console's side: a SAML provider is
// created from the four facts an IdP console publishes, OurWay derives the SP
// identity (an operator cannot type one that points at another installation), and the
// key material never leaves the installation.
func TestSAMLProviderIsBuiltFromTheIdPFacts(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()

	id := f.saveSAMLProvider(t, admin, "corp-saml", idp, nil)

	// The stored row: the derived SP identity, the generated key pair, and no
	// client id — a SAML provider's identity is the entity ID the audience names.
	provider, err := f.st.SSOProviders.GetByID(id)
	if err != nil {
		t.Fatalf("load provider: %v", err)
	}
	if provider.Type != models.SSOTypeSAML {
		t.Fatalf("provider type is %q", provider.Type)
	}
	if provider.SPEntityID != sso.SPFor(provider) {
		t.Fatalf("SP entity id is %q, want the derived %q", provider.SPEntityID, sso.SPFor(provider))
	}
	if provider.SPKeyPEM == "" || provider.SPCertPEM == "" {
		t.Fatal("generate_sp_key was asked for and no signing key was made")
	}
	if provider.ClientID != "" {
		t.Fatal("a SAML provider was given a client id: its identity is the SP entity id")
	}

	// The admin list carries the SAML facts an operator acts on, and none of the
	// key material: the certificate as a fingerprint, the key as a fact.
	_, body, _ := f.do(t, http.MethodGet, "/api/sso/providers", admin, nil)
	text := string(body)
	for _, want := range []string{"idp_entity_id", "acs_url", "idp_certificate_sha256", sso.CertificateFingerprint(idp.CertificatePEM())} {
		if !strings.Contains(text, want) {
			t.Fatalf("the provider list omits %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"BEGIN CERTIFICATE", "PRIVATE KEY", "sp_key_pem", "idp_certificate\":", "sp_cert_pem"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the provider list leaks %q:\n%s", forbidden, text)
		}
	}
}

// TestSAMLProviderRefusesAFormThatCannotDeliver is the save-time gate: a SAML
// provider that cannot answer logins is not saved, and each refusal says what is
// missing.
func TestSAMLProviderRefusesAFormThatCannotDeliver(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()

	cases := []struct {
		name     string
		extra    map[string]any
		contains string
	}{
		{"no IdP entity id", map[string]any{"idp_entity_id": ""}, "idp_entity_id"},
		{"no SSO location", map[string]any{"idp_sso_url": ""}, "idp_sso_url"},
		{"http SSO location", map[string]any{"idp_sso_url": "http://idp.corp.example/sso"}, "https"},
		{"localhost SSO location", map[string]any{"idp_sso_url": "https://localhost:8443/sso"}, "localhost"},
		{"SSO location names a port", map[string]any{"idp_sso_url": "https://idp.corp.example:8443/sso"}, "port"},
		{"SSO location is an IP literal", map[string]any{"idp_sso_url": "https://192.168.4.11/sso"}, "IP address"},
		{"no signing certificate", map[string]any{"idp_certificate": ""}, "idp_certificate"},
		{"certificate that is not a certificate", map[string]any{"idp_certificate": "not a certificate"}, "idp_certificate"},
		{"unknown nameid format", map[string]any{"nameid_format": "unparseable-format"}, "nameid_format"},
		{
			"verified email required with no attribute named",
			map[string]any{"require_verified_email": true},
			"verified_email_attribute",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := f.saveProvider(t, admin, samlProviderForm("refused-saml-"+strings.ReplaceAll(tc.name, " ", "-"), idp, tc.extra))
			if code != http.StatusBadRequest {
				t.Fatalf("save = %d, want 400\n%s", code, body)
			}
			if !strings.Contains(string(body), tc.contains) {
				t.Fatalf("refusal does not name %q:\n%s", tc.contains, body)
			}
			// Nothing was written: a refused form leaves no half-built provider.
			_, list, _ := f.do(t, http.MethodGet, "/api/sso/providers", admin, nil)
			if strings.Contains(string(list), "refused-saml-"+strings.ReplaceAll(tc.name, " ", "-")) {
				t.Fatal("a refused provider form saved a provider row")
			}
		})
	}

	// A SAML provider is not an OIDC provider, in either direction.
	id := f.saveSAMLProvider(t, admin, "corp-saml-fixed", idp, nil)
	code, body := f.editProvider(t, admin, id, map[string]any{"type": "oidc"})
	if code != http.StatusConflict || !strings.Contains(string(body), "type_immutable") {
		t.Fatalf("switching a SAML provider to OIDC = %d\n%s", code, body)
	}
	oidcID := f.saveOIDCProviderRow(t, admin, "oidc-row")
	code, body = f.editProvider(t, admin, oidcID, map[string]any{"type": models.SSOTypeSAML})
	if code != http.StatusConflict || !strings.Contains(string(body), "type_immutable") {
		t.Fatalf("switching an OIDC provider to SAML = %d\n%s", code, body)
	}
}

// saveOIDCProviderRow saves the OIDC provider the SAML tests switch, so the
// type-immutability rule is proven in both directions.
func (f *ssoFixture) saveOIDCProviderRow(t *testing.T, admin, name string) string {
	t.Helper()
	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": name, "client_id": "ourway", "client_secret": "saml-fixture-secret",
	})
	if code != http.StatusOK {
		t.Fatalf("save oidc provider = %d\n%s", code, body)
	}
	var answer struct {
		Provider models.SSOProvider `json:"provider"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	return answer.Provider.ID
}

// TestSAMLLoginIssuesASessionThroughTheExchange is the round trip the acceptance
// criteria name: metadata uploaded, browser redirected with a deflated signed
// request, assertion posted back, session issued by the same one-time-code exchange
// the OIDC flow uses — with no token in any URL, and the ledger saying the identity
// rested on a verified signature.
func TestSAMLLoginIssuesASessionThroughTheExchange(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	f.saveSAMLProvider(t, admin, "corp-saml", idp, nil)
	sp := f.samlSP(t, "corp-saml")

	if !strings.Contains(sp.Metadata, sp.ACSURL) || !strings.Contains(sp.Metadata, sp.EntityID) {
		t.Fatal("the metadata an operator uploads does not name the entity id and ACS the login uses")
	}
	if strings.Contains(sp.Metadata, "PRIVATE KEY") {
		t.Fatal("the metadata carries key material")
	}

	access := f.samlLogin(t, "corp-saml", sp, idp, samlClaims(), testsaml.AssertionOptions{})

	// The session is a session: it reaches the console's own routes, it is filed
	// under the provider that asserted it, and JIT provisioning gave it the floor —
	// a person the IdP has never heard of is not an operator here.
	account, roles := f.profileOf(t, access)
	if account.Provider != "corp-saml" {
		t.Fatalf("the account is filed under provider %q, want corp-saml", account.Provider)
	}
	if account.ProviderID != "ada@corp.example" {
		t.Fatalf("the account is filed under subject %q, want the asserted NameID", account.ProviderID)
	}
	if len(roles) != 1 || roles[0] != "viewer" {
		t.Fatalf("a JIT-provisioned SSO account has roles %v, want the viewer floor", roles)
	}
	if account.Email != "ada@corp.example" {
		t.Fatalf("the account's email is %q, want the asserted address", account.Email)
	}

	rows := rowsFor(t, f, models.AuditAuthLogin)
	var found *models.AuditLog
	for i := range rows {
		var details map[string]any
		if err := json.Unmarshal([]byte(rows[i].Details), &details); err != nil {
			continue
		}
		if details["provider"] == "corp-saml" {
			found = &rows[i]
			if details["assertion_signature_verified"] != true {
				t.Fatalf("the login row does not state the signature was verified: %v", details)
			}
			if details["protocol"] != models.SSOTypeSAML {
				t.Fatalf("the login row does not say the login came through SAML: %v", details)
			}
			if details["subject"] != "ada@corp.example" {
				t.Fatalf("the login row does not name the asserted subject: %v", details)
			}
		}
	}
	if found == nil {
		t.Fatal("a SAML login was not audited")
	}
}

// TestSAMLLoginCannotClaimAnExistingLocalAccount is 2.4-3's gate, applied to SAML:
// the same takeover the OIDC contract closes is closed here with the same refusal,
// the same pending identity, and the same admin doors that resolve it.
func TestSAMLLoginCannotClaimAnExistingLocalAccount(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	target := f.createLocalAccount(t, "ops-admin", "ops-admin@corp.example", "admin")
	idp := testsaml.New()

	// Permissive on purpose: no allow-list, default link policy. The IdP asserts the
	// email address of a local admin.
	f.saveSAMLProvider(t, admin, "permissive-saml", idp, nil)
	sp := f.samlSP(t, "permissive-saml")

	claims := samlClaims()
	claims.NameID = "ops-admin@corp.example"
	claims.Email = "ops-admin@corp.example"
	claims.Username = "ops-admin"
	requestID := f.startSAMLLogin(t, "permissive-saml")
	formValue := idp.ResponseFormValue(claims, testsaml.AssertionOptions{}, sp.ACSURL, requestID, sp.EntityID)
	status, _, reason, _ := f.postSAMLAssertion(t, "permissive-saml", formValue)
	if status != http.StatusForbidden {
		t.Fatalf("acs post = %d (reason %q), want 403: the contract refuses by name", status, reason)
	}
	if reason != "identity_pending" {
		t.Fatalf("refusal reason is %q, want identity_pending", reason)
	}

	if roles := f.roleNamesOf(t, target.ID); len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("the local admin's roles changed: %v", roles)
	}

	pending, body, _ := f.do(t, http.MethodGet, "/api/users/pending", admin, nil)
	if pending != http.StatusOK {
		t.Fatalf("pending identities = %d\n%s", pending, body)
	}
	var list struct {
		Identities []struct {
			ID             string   `json:"id"`
			Email          string   `json:"email"`
			Provider       string   `json:"provider"`
			LinkTargetID   string   `json:"link_target_id"`
			LinkTargetRole []string `json:"link_target_roles"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode pending: %v\n%s", err, body)
	}
	var pendingID string
	for _, row := range list.Identities {
		if row.Email == "ops-admin@corp.example" {
			pendingID = row.ID
			// The band names the account the identity would have claimed: that is
			// the account the operator is deciding about.
			if row.Provider != "permissive-saml" || row.LinkTargetID != target.ID {
				t.Fatalf("the pending row does not name the account it refused to claim: %+v", row)
			}
		}
	}
	if pendingID == "" {
		t.Fatalf("the SSO identity is not in the pending band: %s", body)
	}

	// Approving the identity gives it its own account; it does not promote anything.
	code, approveBody, _ := f.do(t, http.MethodPost, "/api/users/"+pendingID+"/approve", admin, map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("approve = %d\n%s", code, approveBody)
	}
	if roles := f.roleNamesOf(t, target.ID); len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("approving a pending identity changed the local admin's roles: %v", roles)
	}

	access := f.samlLogin(t, "permissive-saml", sp, idp, claims, testsaml.AssertionOptions{})
	account, roles := f.profileOf(t, access)
	if account.Provider != "permissive-saml" {
		t.Fatalf("the approved identity logged in as %q, want its own SSO account", account.Username)
	}
	for _, role := range roles {
		if role == "admin" {
			t.Fatalf("approving a pending identity granted admin: %v", roles)
		}
	}
}

// TestSAMLRoleMappingOwnsTheRolesItNames is 2.4-3's role rule on SAML groups: the
// mapping grants the roles it names while the assertion asserts the group, and takes
// them back when it does not. Nothing else about the account moves.
func TestSAMLRoleMappingOwnsTheRolesItNames(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	f.saveSAMLProvider(t, admin, "mapped-saml", idp, map[string]any{
		"role_mapping": `{"rmm-operators":"manager"}`,
	})
	sp := f.samlSP(t, "mapped-saml")

	access := f.samlLogin(t, "mapped-saml", sp, idp, samlClaims(), testsaml.AssertionOptions{})
	_, roles := f.profileOf(t, access)
	manager := false
	for _, role := range roles {
		if role == "manager" {
			manager = true
		}
	}
	if !manager {
		t.Fatalf("the asserted group did not grant the mapped role: %v", roles)
	}

	// The same person, asserted without the group, is not a manager any more: the
	// mapping owns only the roles it names, and it owns them only while asserted.
	claims := samlClaims()
	claims.Groups = nil
	claims.DisplayName = ""
	second := f.samlLogin(t, "mapped-saml", sp, idp, claims, testsaml.AssertionOptions{})
	_, secondRoles := f.profileOf(t, second)
	for _, role := range secondRoles {
		if role == "manager" {
			t.Fatalf("the mapped role survived an assertion that no longer asserts the group: %v", secondRoles)
		}
	}

	rows := rowsFor(t, f, models.AuditAuthLogin)
	found := false
	for i := range rows {
		var details map[string]any
		if err := json.Unmarshal([]byte(rows[i].Details), &details); err != nil {
			continue
		}
		if details["provider"] == "mapped-saml" && details["role_mapping_applied"] == true {
			found = true
		}
	}
	if !found {
		t.Fatal("a role mapping applied at a SAML login was not audited")
	}
}

// TestSAMLRefusalsAreNamedInTheLedger is the operator-facing half: five different
// broken assertions are five different fixes at the IdP, and the ledger row for each
// names the reason and the sentence.
func TestSAMLRefusalsAreNamedInTheLedger(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	f.saveSAMLProvider(t, admin, "ledger-saml", idp, nil)
	sp := f.samlSP(t, "ledger-saml")

	cases := []struct {
		name   string
		opts   testsaml.AssertionOptions
		mutate func(string) string
		reason string
		status int
	}{
		{
			name:   "unsigned",
			opts:   testsaml.AssertionOptions{NoSignature: true},
			reason: sso.ReasonSAMLUnsigned,
			status: http.StatusUnauthorized,
		},
		{
			name:   "tampered",
			opts:   testsaml.AssertionOptions{},
			mutate: func(xml string) string { return strings.ReplaceAll(xml, "ada@corp.example", "ops-admin@corp.example") },
			reason: sso.ReasonSAMLSignature,
			status: http.StatusUnauthorized,
		},
		{
			name:   "expired",
			opts:   testsaml.AssertionOptions{NotOnOrAfter: time.Now().Add(-30 * time.Minute)},
			reason: sso.ReasonSAMLNotOnOrAfter,
			status: http.StatusUnauthorized,
		},
		{
			name:   "audience",
			opts:   testsaml.AssertionOptions{Audience: "urn:ourway:victim:22222222"},
			reason: sso.ReasonSAMLAudience,
			status: http.StatusUnauthorized,
		},
		{
			name:   "recipient",
			opts:   testsaml.AssertionOptions{Recipient: "https://other.example/api/auth/sso/saml/ledger-saml/acs"},
			reason: sso.ReasonSAMLRecipient,
			status: http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requestID := f.startSAMLLogin(t, "ledger-saml")
			xml := idp.ResponseXML(samlClaims(), tc.opts, sp.ACSURL, requestID, sp.EntityID)
			if tc.mutate != nil {
				xml = tc.mutate(xml)
			}
			formValue := base64.StdEncoding.EncodeToString([]byte(xml))
			status, code, reason, _ := f.postSAMLAssertion(t, "ledger-saml", formValue)
			if status != tc.status {
				t.Fatalf("acs post = %d (reason %q), want %d", status, reason, tc.status)
			}
			if code != "" {
				t.Fatal("a refused assertion issued a login code")
			}
			if reason != tc.reason {
				t.Fatalf("refusal reason is %q, want %q", reason, tc.reason)
			}
			rows := rowsFor(t, f, models.AuditAuthLoginFailed)
			found := false
			for i := range rows {
				var details map[string]any
				if err := json.Unmarshal([]byte(rows[i].Details), &details); err != nil {
					continue
				}
				if details["provider"] != "ledger-saml" || details["reason"] != tc.reason {
					continue
				}
				found = true
				if detail, ok := details["detail"].(string); !ok || strings.TrimSpace(detail) == "" {
					t.Fatalf("the refusal row carries no detail an operator can act on: %v", details)
				}
			}
			if !found {
				t.Fatalf("the refusal was not audited with its reason (%q)", tc.reason)
			}
		})
	}

	// The tampered assertion asserts someone else: the ledger must say who.
	requestID := f.startSAMLLogin(t, "ledger-saml")
	tampered := strings.ReplaceAll(
		idp.ResponseXML(samlClaims(), testsaml.AssertionOptions{}, sp.ACSURL, requestID, sp.EntityID),
		"ada@corp.example", "ops-admin@corp.example")
	f.postSAMLAssertion(t, "ledger-saml", base64.StdEncoding.EncodeToString([]byte(tampered)))
	found := false
	for _, row := range rowsFor(t, f, models.AuditAuthLoginFailed) {
		var details map[string]any
		if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
			continue
		}
		if details["reason"] == sso.ReasonSAMLSignature && details["provider"] == "ledger-saml" {
			found = true
		}
	}
	if !found {
		t.Fatal("the signature refusal is not in the ledger")
	}
}

// TestSAMLReplayedAssertionIsRefused is the bearer-artifact rule at the route: the
// same form value posted twice is one login.
func TestSAMLReplayedAssertionIsRefused(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	f.saveSAMLProvider(t, admin, "replay-saml", idp, nil)
	sp := f.samlSP(t, "replay-saml")

	// An IdP-initiated assertion carries no login state on our side, so the
	// assertion is the *only* thing binding it to a login: this is the shape a
	// captured POST has, and the replay store is what makes it one login.
	formValue := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, sp.ACSURL, "", sp.EntityID)

	status, code, reason, _ := f.postSAMLAssertion(t, "replay-saml", formValue)
	if status != http.StatusFound {
		t.Fatalf("first presentation = %d (reason %q)", status, reason)
	}
	f.redeemSAMLSession(t, code)

	status, code, reason, _ = f.postSAMLAssertion(t, "replay-saml", formValue)
	if status != http.StatusUnauthorized || reason != sso.ReasonSAMLReplayed {
		t.Fatalf("replayed assertion = %d (reason %q), want 401 %s", status, reason, sso.ReasonSAMLReplayed)
	}
	if code != "" {
		t.Fatal("a replayed assertion issued a login code")
	}

	// A captured *SP-initiated* assertion is refused as well, and by the rule that
	// binds it to a login in the first place: the state it answered is consumed, so
	// the second presentation answers a request this installation is not making.
	requestID := f.startSAMLLogin(t, "replay-saml")
	captured := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, sp.ACSURL, requestID, sp.EntityID)
	status, code, reason, _ = f.postSAMLAssertion(t, "replay-saml", captured)
	if status != http.StatusFound {
		t.Fatalf("a fresh SP-initiated assertion = %d (reason %q)", status, reason)
	}
	f.redeemSAMLSession(t, code)
	if status, _, reason, _ = f.postSAMLAssertion(t, "replay-saml", captured); status == http.StatusFound {
		t.Fatal("a captured SP-initiated assertion was replayed")
	}
	found := false
	for _, row := range rowsFor(t, f, models.AuditAuthLoginFailed) {
		var details map[string]any
		if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
			continue
		}
		if details["reason"] == sso.ReasonSAMLReplayed && details["provider"] == "replay-saml" {
			found = true
		}
	}
	if !found {
		t.Fatal("a replayed assertion was not audited")
	}
}

// TestSAMLLoginFeedsTheContractTheProviderConfigures: the 2.4-3 gates are enforced on
// the SAML path with their named refusals and their 403, because an assertion is a
// claim, not a permission.
func TestSAMLLoginFeedsTheContractTheProviderConfigures(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()

	f.saveSAMLProvider(t, admin, "domain-saml", idp, map[string]any{"allowed_domains": "other.example"})
	f.saveSAMLProvider(t, admin, "verified-saml", idp, map[string]any{
		"require_verified_email": true, "verified_email_attribute": "emailVerified",
	})

	spDomain := f.samlSP(t, "domain-saml")
	requestID := f.startSAMLLogin(t, "domain-saml")
	formValue := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, spDomain.ACSURL, requestID, spDomain.EntityID)
	status, _, reason, _ := f.postSAMLAssertion(t, "domain-saml", formValue)
	if status != http.StatusForbidden || reason != "domain_not_allowed" {
		t.Fatalf("domain allow-list = %d (reason %q), want 403 domain_not_allowed", status, reason)
	}

	spVerified := f.samlSP(t, "verified-saml")
	claims := samlClaims()
	claims.VerifiedAttribute = "emailVerified"
	claims.Verified = false
	requestID = f.startSAMLLogin(t, "verified-saml")
	formValue = idp.ResponseFormValue(claims, testsaml.AssertionOptions{}, spVerified.ACSURL, requestID, spVerified.EntityID)
	status, _, reason, _ = f.postSAMLAssertion(t, "verified-saml", formValue)
	if status != http.StatusForbidden || reason != "email_unverified" {
		t.Fatalf("unverified assertion = %d (reason %q), want 403 email_unverified", status, reason)
	}

	// The same provider accepts the assertion once the IdP asserts the verified
	// attribute, which is why the provider names the attribute rather than guessing.
	claims.Verified = true
	requestID = f.startSAMLLogin(t, "verified-saml")
	formValue = idp.ResponseFormValue(claims, testsaml.AssertionOptions{}, spVerified.ACSURL, requestID, spVerified.EntityID)
	if status, _, reason, _ = f.postSAMLAssertion(t, "verified-saml", formValue); status != http.StatusFound {
		t.Fatalf("verified assertion = %d (reason %q), want the login to proceed", status, reason)
	}
}

// TestSAMLIdPInitiatedLoginIsAcceptedOnTheAssertionRules documents the one place the
// SAML path is deliberately looser than the OIDC path: an IdP that starts the login
// posts an assertion with no login state on our side. It is accepted on the audience,
// recipient, destination and expiry rules — which is what makes it *this*
// installation's login — and a state from another provider's login is refused.
func TestSAMLIdPInitiatedLoginIsAcceptedOnTheAssertionRules(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	f.saveSAMLProvider(t, admin, "idp-initiated", idp, nil)
	sp := f.samlSP(t, "idp-initiated")

	formValue := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, sp.ACSURL, "", sp.EntityID)
	status, code, reason, _ := f.postSAMLAssertion(t, "idp-initiated", formValue)
	if status != http.StatusFound {
		t.Fatalf("an IdP-initiated assertion = %d (reason %q), want the login to proceed", status, reason)
	}
	if access := f.redeemSAMLSession(t, code); access == "" {
		t.Fatal("the IdP-initiated login issued no session")
	}

	// An assertion addressed to us with a foreign audience is refused, with no state
	// involved at all: the audience rule is what makes it ours.
	foreign := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{
		Audience: "urn:ourway:other-installation:33333333",
	}, sp.ACSURL, "", sp.EntityID)
	if status, _, reason, _ = f.postSAMLAssertion(t, "idp-initiated", foreign); status != http.StatusUnauthorized ||
		reason != sso.ReasonSAMLAudience {
		t.Fatalf("a foreign-audience assertion = %d (reason %q), want 401 %s", status, reason, sso.ReasonSAMLAudience)
	}
}

// TestSAMLLoginIsScopedToTheProviderItNames: an assertion minted for one provider
// cannot log in through another provider's ACS, and a provider row that is not SAML
// does not answer the ACS at all.
func TestSAMLLoginIsScopedToTheProviderItNames(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	f.saveSAMLProvider(t, admin, "alpha-saml", idp, nil)
	f.saveSAMLProvider(t, admin, "beta-saml", idp, nil)
	oidcID := f.saveOIDCProviderRow(t, admin, "oidc-door")

	alpha := f.samlSP(t, "alpha-saml")
	// No login state involved: the assertion is bound to alpha by its audience,
	// Recipient and Destination alone, which is the case that must fail.
	formValue := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, alpha.ACSURL, "", alpha.EntityID)

	// Same signed document, other provider's door: those three facts name alpha, so
	// beta refuses it.
	status, code, reason, _ := f.postSAMLAssertion(t, "beta-saml", formValue)
	if status == http.StatusFound {
		t.Fatal("an assertion minted for one provider logged in through another")
	}
	if code != "" {
		t.Fatal("a cross-provider assertion issued a login code")
	}
	if reason != sso.ReasonSAMLDestination && reason != sso.ReasonSAMLAudience && reason != sso.ReasonSAMLRecipient {
		t.Fatalf("cross-provider post = %d (reason %q), want the refusal that names the destination", status, reason)
	}

	// The ACS is a SAML door only.
	form := url.Values{}
	form.Set("SAMLResponse", base64.StdEncoding.EncodeToString([]byte("<Response/>")))
	resp, err := f.noRedirect().Post(f.baseURL+"/api/auth/sso/saml/oidc-door/acs",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("acs post: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "provider_not_saml") {
		t.Fatalf("ACS on an OIDC provider = %d\n%s", resp.StatusCode, body)
	}

	// And the OIDC callback is not a SAML door.
	callbackStatus, callbackBody, _ := f.do(t, http.MethodGet, "/api/auth/sso/alpha-saml/callback?code=x&state=y", "", nil)
	if callbackStatus != http.StatusBadRequest || !strings.Contains(string(callbackBody), "saml_uses_acs") {
		t.Fatalf("OIDC callback on a SAML provider = %d\n%s", callbackStatus, callbackBody)
	}
	_ = oidcID
}

// TestSAMLDisabledProviderDoesNotAnswer is the enable toggle: a disabled provider is
// absent from the login page, its authorize refuses, and its ACS refuses. The
// provider row and its key material stay, so the toggle is reversible.
func TestSAMLDisabledProviderDoesNotAnswer(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	id := f.saveSAMLProvider(t, admin, "off-saml", idp, nil)
	sp := f.samlSP(t, "off-saml")

	if code, body := f.editProvider(t, admin, id, map[string]any{"enabled": false}); code != http.StatusOK {
		t.Fatalf("disable = %d\n%s", code, body)
	}
	if providers := f.loginPageProviders(t); len(providers) > 0 && strings.Contains(strings.Join(providers, ","), "off-saml") {
		t.Fatalf("a disabled provider is on the login page: %v", providers)
	}

	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/off-saml/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusFound {
		t.Fatal("a disabled provider redirected the browser to its IdP")
	}
	if resp.StatusCode == http.StatusNotFound {
		// 2.4-1's rule, kept: the login page's lookup is enabled-only, so a
		// provider that is switched off is not a door at all.
		_ = body
	} else if !strings.Contains(string(body), "provider_disabled") {
		t.Fatalf("disabled authorize = %d\n%s", resp.StatusCode, body)
	}

	// The assertion consumer service uses the same enabled-only lookup, so a
	// disabled provider answers a posted assertion with nothing: no session, no
	// login code, no identity.
	formValue := idp.ResponseFormValue(samlClaims(), testsaml.AssertionOptions{}, sp.ACSURL, "", sp.EntityID)
	if status, code, reason, _ := f.postSAMLAssertion(t, "off-saml", formValue); status == http.StatusFound || code != "" {
		t.Fatalf("a disabled provider accepted an assertion (status %d, code %q, reason %q)", status, code, reason)
	}

	// The metadata of a disabled provider is still readable: the operator fixing the
	// provider needs the document, and it exposes nothing secret.
	if status, _, _ := f.samlMetadata(t, "off-saml"); status != http.StatusOK {
		t.Fatalf("metadata of a disabled provider = %d, want 200", status)
	}
	if code, body := f.editProvider(t, admin, id, map[string]any{"enabled": true}); code != http.StatusOK {
		t.Fatalf("re-enable = %d\n%s", code, body)
	}
	if access := f.samlLogin(t, "off-saml", sp, idp, samlClaims(), testsaml.AssertionOptions{}); access == "" {
		t.Fatal("the re-enabled provider cannot log anyone in")
	}
}

// TestSAMLTestProbeAnswersTheQuestionsSAMLCanAnswer is the provider ledger's test
// button: certificate expiry, the ACS URL, the SP entity id, the metadata document,
// and signed requests. A certificate about to expire is a wave of refused logins, and
// the probe is where the operator hears it.
func TestSAMLTestProbeAnswersTheQuestionsSAMLCanAnswer(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	id := f.saveSAMLProvider(t, admin, "probe-saml", idp, nil)

	code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/"+id+"/test", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("test = %d\n%s", code, body)
	}
	text := string(body)
	for _, want := range []string{"idp_certificate", "acs_url", "sp_entity_id", "metadata", "signed_requests", sso.CertificateFingerprint(idp.CertificatePEM())} {
		if !strings.Contains(text, want) {
			t.Fatalf("the SAML probe omits %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"state":"fail"`) {
		t.Fatalf("the probe reports a failure on a correctly configured provider:\n%s", text)
	}

	// An expired certificate is stated as a failure, with the date.
	expiredIdP := testsaml.New()
	expiredIdP.SetClock(func() time.Time { return time.Now().AddDate(-3, 0, 0) })
	expiredID := f.saveSAMLProvider(t, admin, "expired-saml", expiredIdP, nil)
	if _, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/"+expiredID+"/test", admin, nil); !strings.Contains(string(body), "expired") {
		t.Fatalf("the probe does not state an expired certificate:\n%s", body)
	}
}

// TestSAMLWritesAreAuditedWithTheFingerprintAndNeverTheKey: changing who can sign
// assertions for an installation is a ledger row, and the row records the
// certificate as the value an operator compares — never the PEM, never the key.
func TestSAMLWritesAreAuditedWithTheFingerprintAndNeverTheKey(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-admin")
	idp := testsaml.New()
	id := f.saveSAMLProvider(t, admin, "audit-saml", idp, nil)
	other := testsaml.New()

	if code, body := f.editProvider(t, admin, id, map[string]any{
		"idp_certificate": other.CertificatePEM(),
		"enabled":         true,
	}); code != http.StatusOK {
		t.Fatalf("rotate the certificate = %d\n%s", code, body)
	}

	rows := rowsFor(t, f, models.AuditSSOUpdate)
	if len(rows) == 0 {
		t.Fatal("a SAML provider edit was not audited")
	}
	var details map[string]any
	for i := range rows {
		if err := json.Unmarshal([]byte(rows[i].Details), &details); err != nil {
			continue
		}
		if details["name"] == "audit-saml" {
			break
		}
	}
	if details["type"] != models.SSOTypeSAML {
		t.Fatalf("the provider row does not say it is SAML: %v", details)
	}
	if details["idp_certificate_sha256"] != sso.CertificateFingerprint(other.CertificatePEM()) {
		t.Fatalf("the row does not name the new certificate's fingerprint: %v", details)
	}
	if details["sp_entity_id"] == "" || details["sp_entity_id"] == nil {
		t.Fatalf("the row does not name the SP entity id: %v", details)
	}

	// No provider row in the ledger carries key material.
	for _, row := range rowsFor(t, f, models.AuditSSOUpdate) {
		if strings.Contains(row.Details, "BEGIN CERTIFICATE") || strings.Contains(row.Details, "PRIVATE KEY") {
			t.Fatalf("the provider ledger carries key material: %s", row.Details)
		}
	}
	for _, row := range rowsFor(t, f, models.AuditSSOCreate) {
		if strings.Contains(row.Details, "BEGIN CERTIFICATE") || strings.Contains(row.Details, "PRIVATE KEY") {
			t.Fatalf("the provider ledger carries key material: %s", row.Details)
		}
	}
}

// TestAProviderFromBeforeSAMLKeepsLoggingPeopleIn is 2.4-7's upgrade test: an
// installation that runs an OIDC provider today has provider rows, accounts filed by
// subject, and a login page. After SAML ships, that provider still logs people in,
// its row is unchanged in every field SAML touched, and the SAML provider can be
// added beside it — with the login page listing both.
func TestAProviderFromBeforeSAMLKeepsLoggingPeopleIn(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "saml-upgrade-admin")
	idp := testidp.New()
	defer idp.Close()

	f.saveOIDCProvider(t, admin, "upgrade-oidc", idp.Issuer())
	access := f.loginThrough(t, "upgrade-oidc", idp, testidp.Claims{
		Subject: "upgrade-subject", Email: "upgrade@corp.example", EmailVerified: true, Name: "Up Grade",
	})
	account, roles := f.profileOf(t, access)
	if account.Provider != "upgrade-oidc" {
		t.Fatalf("the pre-SAML provider filed its account under %q", account.Provider)
	}
	if account.ProviderID != "upgrade-subject" {
		t.Fatalf("the account's subject is %q, want the OIDC subject", account.ProviderID)
	}
	if len(roles) != 1 || roles[0] != "viewer" {
		t.Fatalf("the pre-SAML provider's JIT floor changed: %v", roles)
	}

	// The OIDC row carries no SAML facts, and the console says so by omission: a
	// provider row that does not speak SAML does not get a SAML panel.
	_, body, _ := f.do(t, http.MethodGet, "/api/sso/providers", admin, nil)
	var listed struct {
		Providers []struct {
			models.SSOProvider
			SAML *json.RawMessage `json:"saml"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode provider list: %v\n%s", err, body)
	}
	for _, row := range listed.Providers {
		if row.Name == "upgrade-oidc" {
			if row.SAML != nil {
				t.Fatalf("an OIDC provider row carries a SAML panel: %s", *row.SAML)
			}
			if row.NameIDFormat != "emailaddress" {
				t.Fatalf("the pre-SAML row's nameid_format is %q: the upgrade must leave a value SAML's code treats as its default", row.NameIDFormat)
			}
			if row.LinkPolicy != models.SSOLinkPolicyPending {
				t.Fatalf("the pre-SAML row's identity contract changed to %q", row.LinkPolicy)
			}
		}
	}

	// SAML arrives beside it: the login page lists both, in the operator's order.
	samlIdP := testsaml.New()
	f.saveSAMLProvider(t, admin, "upgrade-saml", samlIdP, nil)
	names := strings.Join(f.loginPageProviders(t), ",")
	if !strings.Contains(names, "upgrade-oidc") || !strings.Contains(names, "upgrade-saml") {
		t.Fatalf("the login page lists %q, want both providers", names)
	}

	sp := f.samlSP(t, "upgrade-saml")
	if access := f.samlLogin(t, "upgrade-saml", sp, samlIdP, samlClaims(), testsaml.AssertionOptions{}); access == "" {
		t.Fatal("the SAML provider added after the upgrade cannot log anyone in")
	}
	access = f.loginThrough(t, "upgrade-oidc", idp, testidp.Claims{
		Subject: "upgrade-subject", Email: "upgrade@corp.example", EmailVerified: true,
	})
	if _, roles := f.profileOf(t, access); len(roles) == 0 {
		t.Fatal("the pre-SAML provider stopped logging its account in")
	}
}
