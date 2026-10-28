package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"

	"gorm.io/driver/sqlite"
)

// 2.3-4c — a tenant's enrollment token, on the real router.
//
// The surface being tested is a *credential*: the claims are "it places the unit
// in the tenant it names", "the pre-2.3 path is byte-for-byte unchanged", and "an
// inert token is indistinguishable from one that never existed". Those are
// properties of the whole enrollment path — header, hash lookup, tenant column —
// and a store-level test cannot see the header.
//
// `newTestServer` runs with no enrollment secret (the open-installment shape every
// other suite uses), so this fixture builds its own router with one: with the
// gate closed, the token path is the only door, which is the configuration where
// a mistake here would be a break-in.

const enrollTestSecret = "install-wide-secret"

type enrollFixture struct {
	baseURL string
	st      *store.Store
	alice   *models.Client
	bob     *models.Client
	admin   string // admin token
	manager string // manager scoped to alice
}

func newEnrollFixture(t *testing.T) *enrollFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub("http://localhost:3000", "")
	engine := alerts.NewEngine(st.Alerts)
	router := SetupRouter(context.Background(), st, jwtAuth, hub, engine, "http://localhost:3000", enrollTestSecret, 100, 0)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	alice, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha Co", Slug: "alpha-co"})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	bob, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Beta Co", Slug: "beta-co"})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}

	sf := &scopeFixture{baseURL: ts.URL, st: st}
	f := &enrollFixture{
		baseURL: ts.URL,
		st:      st,
		alice:   alice,
		bob:     bob,
		admin:   sf.makeUserWithScope(t, "token-admin", "admin"),
	}
	f.manager = sf.makeUserWithScope(t, "token-manager", "manager", alice.ID)
	return f
}

// mint creates a token through the admin surface and returns the plaintext the
// response carried — the only place it exists.
func (f *enrollFixture) mint(t *testing.T, clientID string, body map[string]any) (int, string, map[string]any) {
	t.Helper()
	code, raw, _ := (&scopeFixture{baseURL: f.baseURL, st: f.st}).do(t, http.MethodPost, f.baseURL+"/api/clients/"+clientID+"/tokens", f.admin, body)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	value, _ := env["token_value"].(string)
	return code, value, env
}

// register runs the agent's registration with a credential in the header.
func (f *enrollFixture) register(t *testing.T, credential, hostname string) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name": hostname, "hostname": hostname, "os": "linux", "arch": "amd64",
		"agent_version": "2.3.0", "public_ip": "203.0.113.7", "private_ip": "10.70." + hostname,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, f.baseURL+"/api/agent/register", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if credential != "" {
		req.Header.Set("X-Enrollment-Secret", credential)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	raw := make([]byte, 0, 256)
	buf := make([]byte, 64)
	for {
		n, rerr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, raw
}

func (f *enrollFixture) deviceID(t *testing.T, raw []byte) string {
	t.Helper()
	var env struct {
		Device struct {
			ID       string  `json:"id"`
			ClientID *string `json:"client_id"`
		} `json:"device"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode register response: %v (%s)", err, raw)
	}
	return env.Device.ID
}

func TestEnrollmentTokenPlacesTheUnitInTheTenant(t *testing.T) {
	f := newEnrollFixture(t)

	code, value, env := f.mint(t, f.alice.ID, map[string]any{"label": "front desk image"})
	if code != http.StatusCreated {
		t.Fatalf("mint = %d: %v", code, env)
	}
	if value == "" {
		t.Fatal("the create response carries no token_value")
	}
	tokID, _ := env["token"].(map[string]any)["id"].(string)
	if tokID == "" {
		t.Fatalf("the create response carries no token id: %v", env)
	}

	code, raw := f.register(t, value, "unit-token-a")
	if code != http.StatusCreated {
		t.Fatalf("register with a tenant token = %d: %s", code, raw)
	}
	id := f.deviceID(t, raw)
	dev, err := f.st.Devices.GetByID(id)
	if err != nil || dev == nil {
		t.Fatalf("the enrolled unit is unreadable: %v %+v", err, dev)
	}
	if dev.ClientID == nil || *dev.ClientID != f.alice.ID {
		t.Fatalf("the enrolled unit is %v, want it in alpha (%s)", dev.ClientID, f.alice.ID)
	}

	// The list is a ledger of credentials, not a source of them.
	var listEnv struct {
		Tokens []map[string]any `json:"tokens"`
		Total  int              `json:"total"`
	}
	sf := &scopeFixture{baseURL: f.baseURL, st: f.st}
	lcode, lraw, _ := sf.get(t, "/api/clients/"+f.alice.ID+"/tokens", f.admin)
	if lcode != http.StatusOK {
		t.Fatalf("list tokens = %d: %s", lcode, lraw)
	}
	if err := json.Unmarshal(lraw, &listEnv); err != nil {
		t.Fatalf("decode token list: %v", err)
	}
	if listEnv.Total != 1 || len(listEnv.Tokens) != 1 {
		t.Fatalf("token list = %d/%d, want 1 row", len(listEnv.Tokens), listEnv.Total)
	}
	row := listEnv.Tokens[0]
	if row["label"] != "front desk image" || row["id"] != tokID {
		t.Errorf("token row = %v, want this run's token", row)
	}
	for _, banned := range []string{"token", "token_value", "token_hash", "key"} {
		if _, ok := row[banned]; ok {
			t.Errorf("the token list carries %q: %v", banned, row)
		}
	}
	if _, ok := row["created_at"]; !ok {
		t.Error("the token list carries no creation time")
	}

	// The mint is a record, on the tenant it belongs to, naming no secret.
	var create *models.AuditLog
	for _, r := range auditRows(t, f.st, 40) {
		if r.Action == models.AuditClientTokenCreate {
			row := r
			create = &row
		}
	}
	if create == nil {
		t.Fatal("minting an enrollment token wrote no audit row")
	}
	if create.ClientID != f.alice.ID || create.TargetID != f.alice.ID {
		t.Errorf("mint row targets %s/%s, want alpha %s", create.TargetType, create.TargetID, f.alice.ID)
	}
	if create.Outcome != models.AuditOutcomeOK {
		t.Errorf("mint row outcome = %q, want ok", create.Outcome)
	}
	if got := create.Details; got == "" {
		t.Error("the mint row has no details")
	} else {
		for _, banned := range []string{value, "token_"} {
			if strings.Contains(got, banned) {
				t.Errorf("the mint row contains %q — the ledger must not carry the secret", banned)
			}
		}
	}

	// The last-used stamp is a read the operator makes before rotating a token.
	list, err := f.st.ClientTokens.ListByClient(f.alice.ID)
	if err != nil || len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("the enrollment did not stamp the token as used: %v %+v", err, list)
	}
}

func TestInertEnrollmentTokenAnswersAsOneThatNeverExisted(t *testing.T) {
	f := newEnrollFixture(t)

	// Expired: minted with a window, and the window has passed. The store is the
	// clock here — a test cannot sleep for `expires_in_days`.
	past := time.Now().UTC().Add(-time.Minute)
	expired := &models.ClientEnrollmentToken{ClientID: f.alice.ID, Label: "yesterday", ExpiresAt: &past}
	if err := f.st.ClientTokens.Create(expired); err != nil {
		t.Fatalf("create expired token: %v", err)
	}
	// Revoked: minted live through the API, then revoked through it.
	code, live, _ := f.mint(t, f.bob.ID, map[string]any{"label": "field kit"})
	if code != http.StatusCreated {
		t.Fatalf("mint = %d", code)
	}
	var list struct {
		Tokens []struct {
			ID string `json:"id"`
		} `json:"tokens"`
	}
	sf := &scopeFixture{baseURL: f.baseURL, st: f.st}
	_, lraw, _ := sf.get(t, "/api/clients/"+f.bob.ID+"/tokens", f.admin)
	if err := json.Unmarshal(lraw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Tokens) < 1 {
		t.Fatal("the minted token is not listed")
	}
	revokeCode, _, _ := sf.do(t, http.MethodDelete, f.baseURL+"/api/clients/"+f.bob.ID+"/tokens/"+list.Tokens[0].ID, f.admin, nil)
	if revokeCode != http.StatusOK {
		t.Fatalf("revoke = %d", revokeCode)
	}

	bodies := map[string]string{}
	for name, credential := range map[string]string{
		"expired":   expired.Token,
		"revoked":   live,
		"nonsense":  "owt_" + uuid.NewString(),
		"absent":    "",
		"globalish": enrollTestSecret + "x",
	} {
		code, raw := f.register(t, credential, "unit-inert-"+name)
		bodies[name] = string(raw)
		if name == "absent" || name == "globalish" {
			if code != http.StatusUnauthorized {
				t.Fatalf("register with %s = %d, want 401 (%s)", name, code, raw)
			}
			continue
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("register with %s token = %d, want 401 (%s)", name, code, raw)
		}
	}
	// The refusals are indistinguishable. A registration endpoint that answered
	// "expired" versus "unknown" is a probe for which tokens exist.
	if bodies["expired"] != bodies["nonsense"] {
		t.Errorf("expired answers %q, an unknown token answers %q — the two must be identical",
			bodies["expired"], bodies["nonsense"])
	}
	if bodies["revoked"] != bodies["nonsense"] {
		t.Errorf("revoked answers %q, an unknown token answers %q — the two must be identical",
			bodies["revoked"], bodies["nonsense"])
	}

	// Nothing enrolled: every refusal above answered before any row was written.

	// The revocation is a record on the tenant whose credential it killed.
	var revoked *models.AuditLog
	for _, r := range auditRows(t, f.st, 60) {
		if r.Action == models.AuditClientTokenRevoke {
			row := r
			revoked = &row
		}
	}
	if revoked == nil {
		t.Fatal("revoking an enrollment token wrote no audit row")
	}
	if revoked.ClientID != f.bob.ID || revoked.Outcome != models.AuditOutcomeOK {
		t.Errorf("revoke row = %s / %s, want bob / ok", revoked.ClientID, revoked.Outcome)
	}
}

func TestGlobalEnrollmentSecretIsUnchangedByTokens(t *testing.T) {
	f := newEnrollFixture(t)

	code, raw := f.register(t, enrollTestSecret, "unit-global")
	if code != http.StatusCreated {
		t.Fatalf("register with the installation secret = %d: %s", code, raw)
	}
	dev, err := f.st.Devices.GetByID(f.deviceID(t, raw))
	if err != nil || dev == nil {
		t.Fatalf("the unit enrolled with the installation secret is unreadable: %v %+v", err, dev)
	}
	// The pre-2.3 meaning survives exactly: the unit joins unassigned. A token
	// feature that made the installation secret assign tenants would be a
	// behaviour change in the agent pairing, and 2.3's wire contract says
	// agents are unaffected.
	if dev.ClientID != nil {
		t.Fatalf("the installation secret placed the unit in client %v; it must leave it unassigned", *dev.ClientID)
	}
}

func TestEnrollmentTokenRoutesAreAdminOnly(t *testing.T) {
	f := newEnrollFixture(t)
	sf := &scopeFixture{baseURL: f.baseURL, st: f.st}

	// A manager of alpha may read alpha; a credential is not a read.
	if code, _, _ := sf.get(t, "/api/clients/"+f.alice.ID+"/tokens", f.manager); code != http.StatusForbidden {
		t.Errorf("manager GET tokens = %d, want 403", code)
	}
	if code, _, _ := sf.do(t, http.MethodPost, f.baseURL+"/api/clients/"+f.alice.ID+"/tokens", f.manager, map[string]any{"label": "sneaky"}); code != http.StatusForbidden {
		t.Errorf("manager POST tokens = %d, want 403", code)
	}
	// And a manager of alpha may not revoke a token of beta.
	code, live, _ := f.mint(t, f.bob.ID, map[string]any{"label": "beta's"})
	if code != http.StatusCreated {
		t.Fatalf("mint = %d", code)
	}
	_ = live
	var list struct {
		Tokens []struct {
			ID string `json:"id"`
		} `json:"tokens"`
	}
	_, lraw, _ := sf.get(t, "/api/clients/"+f.bob.ID+"/tokens", f.admin)
	if err := json.Unmarshal(lraw, &list); err != nil || len(list.Tokens) == 0 {
		t.Fatalf("beta's token is not listed: %v", err)
	}
	if code, _, _ := sf.do(t, http.MethodDelete, f.baseURL+"/api/clients/"+f.bob.ID+"/tokens/"+list.Tokens[0].ID, f.manager, nil); code != http.StatusForbidden {
		t.Errorf("manager DELETE beta's token = %d, want 403", code)
	}

	// Every refusal is a row: the manager pressing a credential route is the
	// event an audit ledger is read for.
	var refused int
	for _, r := range auditRows(t, f.st, 60) {
		if r.Action == models.AuditClientTokenCreate && r.ActorUsername == "token-manager" {
			refused++
		}
	}
	if refused == 0 {
		t.Error("the manager's mint attempts wrote no rows")
	}
}

func TestEnrollmentTokenExpiryIsValidated(t *testing.T) {
	f := newEnrollFixture(t)

	for _, days := range []int{0, -3, 4000} {
		code, _, env := f.mint(t, f.alice.ID, map[string]any{"label": "bounded", "expires_in_days": days})
		if code != http.StatusBadRequest {
			t.Errorf("mint with expires_in_days=%d = %d, want 400 (%v)", days, code, env)
		}
	}
	// An explicit, in-range window is stored.
	code, _, env := f.mint(t, f.alice.ID, map[string]any{"label": "30 days", "expires_in_days": 30})
	if code != http.StatusCreated {
		t.Fatalf("mint with a 30-day window = %d: %v", code, env)
	}
	tok, _ := env["token"].(map[string]any)
	if tok["expires_at"] == nil || tok["expires_at"] == "" {
		t.Errorf("the 30-day token carries no expiry: %v", env["token"])
	}
	// Omitting the window is a choice, not an accident.
	code, _, env = f.mint(t, f.alice.ID, map[string]any{"label": "image build"})
	if code != http.StatusCreated {
		t.Fatalf("mint without a window = %d: %v", code, env)
	}
	tok, _ = env["token"].(map[string]any)
	if v, ok := tok["expires_at"]; !ok || (v != nil && v != "") {
		t.Errorf("a token minted without a window carries an expiry: %v", env["token"])
	}
}

func TestReRegistrationWithClientTokenAssignsOnlyUnassignedUnits(t *testing.T) {
	f := newEnrollFixture(t)

	// A unit that already has a tenant, and a unit that has none.
	inBob := &models.Device{
		ID: uuid.NewString(), Name: "placed", Hostname: "unit-placed", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: "key-placed", ClientID: &f.bob.ID,
		// The registration lookup is (hostname, private_ip): a seeded unit with
		// no address is not the machine the agent is calling for, and the test
		// would silently measure a first enrollment instead of a re-registration.
		PrivateIP: "10.70.unit-placed",
	}
	free := &models.Device{
		ID: uuid.NewString(), Name: "free", Hostname: "unit-free", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: "key-free",
		PrivateIP: "10.70.unit-free",
	}
	for _, d := range []*models.Device{inBob, free} {
		if err := f.st.Devices.Create(d); err != nil {
			t.Fatalf("seed %s: %v", d.Name, err)
		}
	}
	inAlpha := &models.Device{
		ID: uuid.NewString(), Name: "alpha's", Hostname: "unit-alpha-owned", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: "key-alpha", ClientID: &f.alice.ID,
		PrivateIP: "10.70.unit-alpha-owned",
	}
	if err := f.st.Devices.Create(inAlpha); err != nil {
		t.Fatalf("seed alpha's unit: %v", err)
	}

	code, alpha, _ := f.mint(t, f.alice.ID, map[string]any{"label": "re-register"})
	if code != http.StatusCreated {
		t.Fatalf("mint = %d", code)
	}

	// Re-registration is the installer run twice. Alpha's token proves the
	// operator, and must not reach a unit the installation placed in beta: the C4
	// gate exists so hostname + IP cannot adopt an identity, and a tenant
	// credential that opened it for **any** unit would be a skeleton key built
	// out of a customer's credential.
	code, raw := f.register(t, alpha, "unit-placed")
	if code != http.StatusUnauthorized {
		t.Fatalf("re-registering a placed unit with a foreign tenant token = %d, want 401 (%s)", code, raw)
	}
	// The same token re-registers its own tenant's unit, and leaves it there.
	code, raw = f.register(t, alpha, "unit-alpha-owned")
	if code != http.StatusOK {
		t.Fatalf("re-registering alpha's unit with alpha's token = %d, want 200 (%s)", code, raw)
	}
	if got, _ := f.st.Devices.GetByID(inAlpha.ID); got.ClientID == nil || *got.ClientID != f.alice.ID {
		t.Errorf("alpha's re-registered unit left alpha: %v", got.ClientID)
	}
	// The installation secret keeps the pre-2.3 right of way: it re-registers
	// beta's unit and moves nothing.
	code, raw = f.register(t, enrollTestSecret, "unit-placed")
	if code != http.StatusOK {
		t.Fatalf("re-registering with the installation secret = %d, want 200 (%s)", code, raw)
	}
	// The C4 guard's own path is intact: with the key, the unit is refreshed and
	// stays where the admin put it.
	regBody, _ := json.Marshal(map[string]any{
		"name": "placed", "hostname": "unit-placed", "os": "linux", "arch": "amd64",
		"private_ip": "10.70.unit-placed", "device_key": inBob.DeviceKey,
	})
	req, _ := http.NewRequest(http.MethodPost, f.baseURL+"/api/agent/register", bytes.NewReader(regBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-register with the device key = %d, want 200", resp.StatusCode)
	}
	if got, _ := f.st.Devices.GetByID(inBob.ID); got.ClientID == nil || *got.ClientID != f.bob.ID {
		t.Errorf("the re-registered unit moved out of beta: %v", got.ClientID)
	}

	// The unassigned unit is a different case: the credential names its tenant.
	// It is a re-registration (the machine is known by hostname + IP), so the
	// answer is 200 with the unit's original key — and the unit moves from
	// unassigned into the tenant its credential names.
	code, raw = f.register(t, alpha, "unit-free")
	if code != http.StatusOK {
		t.Fatalf("re-registering the free unit with alpha's token = %d, want 200: %s", code, raw)
	}
	got, err := f.st.Devices.GetByID(free.ID)
	if err != nil || got == nil {
		t.Fatalf("the free unit is unreadable after enrollment: %v %+v", err, got)
	}
	if got.ClientID == nil || *got.ClientID != f.alice.ID {
		t.Fatalf("the free unit enrolled as %v, want alpha", got.ClientID)
	}
}
