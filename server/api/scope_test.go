package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.3-1 — the scope resolver, tested over the real router with real tokens.
//
// The rule under test is the one the release turns on: **an out-of-scope device
// answers 404 with the identical body a nonexistent device gets.** Not "a 404 of
// a similar shape" — the assertion compares the bytes, because the difference a
// support engineer can read is a difference an attacker can read, and what they
// can read is which unit IDs exist in someone else's client.

type scopeFixture struct {
	baseURL    string
	st         *store.Store
	alice      *models.Client // technician is scoped to alice
	bob        *models.Client // and must not see bob
	inAlice    string
	inBob      string
	unassigned string
}

// newScopeFixture seeds two clients, a unit in each, a unit nobody has assigned,
// and a technician whose scope is exactly one of them.
func newScopeFixture(t *testing.T) *scopeFixture {
	ts, st, _ := newTestServer(t)

	alice, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha Co", Slug: "alpha-co"})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	bob, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Beta Co", Slug: "beta-co"})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}

	mk := func(name string, clientID *string) string {
		d := &models.Device{
			ID: uuid.NewString(), Name: name, Hostname: name, OS: "linux", Arch: "amd64",
			Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(), ClientID: clientID,
		}
		if err := st.Devices.Create(d); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return d.ID
	}

	return &scopeFixture{
		baseURL:    ts.URL,
		st:         st,
		alice:      alice,
		bob:        bob,
		inAlice:    mk("unit-alpha", &alice.ID),
		inBob:      mk("unit-beta", &bob.ID),
		unassigned: mk("unit-free", nil),
	}
}

// makeUserWithScope creates an account directly in the store, grants its role
// and its client scope, then logs in for a real token.
//
// Directly, because the registration endpoint is a bootstrap: the *first*
// account in an installation gets `admin` (so a fresh deployment is
// manageable). A fixture that registers its technicians first would be testing
// admins while it thinks it is testing technicians — the shape of this exact
// bug is why the resolver's tests name their users' roles out loud.
func (f *scopeFixture) makeUserWithScope(t *testing.T, username, role string, clientIDs ...string) string {
	t.Helper()
	password := "Passw0rd-2.3!"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := &models.User{
		ID:           uuid.NewString(),
		Username:     username,
		Email:        username + "@example.test",
		PasswordHash: string(hash),
		Provider:     "local",
	}
	if err := f.st.Users.Create(user); err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	roleRow, err := f.st.Roles.GetRoleByName(role)
	if err != nil {
		t.Fatalf("role %s not seeded: %v", role, err)
	}
	if err := f.st.UserRoles.AssignRole(user.ID, roleRow.ID); err != nil {
		t.Fatalf("assign role to %s: %v", username, err)
	}
	for _, cid := range clientIDs {
		if err := f.st.Clients.AddUserClient(user.ID, cid); err != nil {
			t.Fatalf("scope %s to %s: %v", username, cid, err)
		}
	}
	return loginUser(t, f.baseURL, username, password)
}

func (f *scopeFixture) get(t *testing.T, path, token string) (int, []byte, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.baseURL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, body, res.Header.Get("X-OurWay-Client-Scope")
}

// The acceptance, byte-for-byte: the answer for a device in another client is
// indistinguishable from the answer for a device that never existed.
func TestScopeOutOfWorksDeviceAnswersTheSameNotFound(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "scoped-tech", "technician", f.alice.ID)

	_, missingBody, _ := f.get(t, "/api/devices/"+uuid.NewString(), token)
	code, outOfScopeBody, _ := f.get(t, "/api/devices/"+f.inBob, token)

	if code != http.StatusNotFound {
		t.Fatalf("out-of-scope device = %d, want 404", code)
	}
	if bytes.Equal(missingBody, outOfScopeBody) == false {
		t.Fatalf("bodies differ.\n nonexistent: %s\n out of scope: %s", missingBody, outOfScopeBody)
	}

	// And the device in scope is simply served.
	code, body, header := f.get(t, "/api/devices/"+f.inAlice, token)
	if code != http.StatusOK {
		t.Fatalf("in-scope device = %d, want 200: %s", code, body)
	}
	if header != f.alice.ID {
		t.Fatalf("scope header = %q, want the caller's one client %q", header, f.alice.ID)
	}
}

// A scoped list contains no foreign rows, and says why on the response.
func TestScopeListIsFilteredAndNamesTheScope(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "list-tech", "technician", f.alice.ID)

	code, body, header := f.get(t, "/api/devices", token)
	if code != http.StatusOK {
		t.Fatalf("list = %d, want 200", code)
	}
	// Decode through the envelope the endpoint actually returns.
	var env struct {
		Devices []models.Device `json:"devices"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode scoped list: %v", err)
	}
	devices := env.Devices
	for _, d := range devices {
		if d.ID == f.inBob {
			t.Fatal("the scoped list contained a beta unit")
		}
		if d.ID == f.unassigned {
			t.Fatal("the scoped list contained the unassigned unit: a client grant is not a grant over the unassigned pool")
		}
	}
	if len(devices) != 1 || devices[0].ID != f.inAlice {
		t.Fatalf("scoped list = %+v, want exactly the alpha unit", devices)
	}
	if header != f.alice.ID {
		t.Fatalf("scope header = %q, want %q", header, f.alice.ID)
	}
}

// Admin is scope-all, on the header and on the row count.
func TestScopeAdminSeesEveryClientAndUnassigned(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "boss", "admin")

	code, body, header := f.get(t, "/api/devices", token)
	if code != http.StatusOK {
		t.Fatalf("admin list = %d, want 200", code)
	}
	if header != "all" {
		t.Fatalf("admin scope header = %q, want \"all\"", header)
	}
	var env struct {
		Devices []models.Device `json:"devices"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if len(env.Devices) != 3 {
		t.Fatalf("admin sees %d devices, want all 3 (two clients + unassigned)", len(env.Devices))
	}
}

// The criterion that decides how scope is stored: a token that is still valid
// must stop working the moment the scope is revoked, without re-login.
func TestScopeRevocationTakesEffectOnTheNextRequest(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "revoked-tech", "technician", f.alice.ID)

	if code, _, _ := f.get(t, "/api/devices/"+f.inAlice, token); code != http.StatusOK {
		t.Fatalf("before revocation, in-scope device = %d, want 200", code)
	}

	user, err := f.st.Users.GetByUsername("revoked-tech")
	if err != nil {
		t.Fatalf("user lookup: %v", err)
	}
	if err := f.st.Clients.RemoveUserClient(user.ID, f.alice.ID); err != nil {
		t.Fatalf("revoke scope: %v", err)
	}

	// Same access token. The 404 shape is the point again: the unit is now
	// simply not there for this caller.
	code, body, header := f.get(t, "/api/devices/"+f.inAlice, token)
	if code != http.StatusNotFound {
		t.Fatalf("after revocation, device = %d, want 404: %s", code, body)
	}
	if header != "none" {
		t.Fatalf("scope header after revocation = %q, want \"none\"", header)
	}

	code, _, _ = f.get(t, "/api/devices", token)
	if code != http.StatusOK {
		t.Fatalf("scoped list after revocation = %d, want 200 (an empty fleet is an answer)", code)
	}
}

// A technician with no client rows sees nothing. This is the case a permissive
// resolver turns into "sees everything".
func TestScopeNoGrantsSeesNothing(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "homeless-tech", "technician")

	code, body, header := f.get(t, "/api/devices", token)
	if code != http.StatusOK {
		t.Fatalf("unscoped list = %d, want 200", code)
	}
	if header != "none" {
		t.Fatalf("unscoped header = %q, want \"none\"", header)
	}
	var env struct {
		Devices []models.Device `json:"devices"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode unscoped list: %v", err)
	}
	if len(env.Devices) != 0 {
		t.Fatalf("a technician with no client saw %d devices, want 0", len(env.Devices))
	}

	for _, id := range []string{f.inAlice, f.inBob, f.unassigned} {
		if code, _, _ := f.get(t, "/api/devices/"+id, token); code != http.StatusNotFound {
			t.Fatalf("unscoped caller fetched device %s with %d, want 404", id, code)
		}
	}
}

// Device history is the unit: scoping the unit scopes its history.
func TestScopeDeviceHistoryFollowsTheUnit(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "history-tech", "technician", f.alice.ID)

	if code, _, _ := f.get(t, "/api/devices/"+f.inAlice+"/metrics/history", token); code != http.StatusOK {
		t.Fatalf("in-scope history = %d, want 200", code)
	}
	if code, _, _ := f.get(t, "/api/devices/"+f.inBob+"/metrics/history", token); code != http.StatusNotFound {
		t.Fatalf("out-of-scope history = %d, want 404", code)
	}
}
