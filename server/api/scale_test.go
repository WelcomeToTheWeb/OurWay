package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"

	"golang.org/x/crypto/bcrypt"
)

// 2.3-8 — the cost of scoping, measured.
//
// Tenancy is a rule applied to every read, so its price is the release's risk:
// a scope check per row turns a technician's fleet list into a storm of queries,
// and `device_id IN (…2,000 ids…)` turns one statement into 2,000 bind
// parameters. Both shapes pass a correctness suite and fail at MSP scale.
//
// So the suite counts. `countQueries` records every statement gorm executes on
// the test database, and what it records is the two numbers the acceptance
// criteria name: how many statements a request costs, and how many bind
// parameters its widest statement carries. A test here fails if either grows
// with the size of the fleet the caller is scoped to, or with the number of
// targets in a bulk command.

// queryCounter counts the statements gorm runs on `db`.
//
// The counter is shared by every goroutine the fixture's router owns — the
// request's own goroutine, the hub, and `SetupRouter`'s background jobs — so it
// is guarded. `go test -race` is the CI server suite, and a measurement harness
// whose numbers come from a data race is not a measurement: the same rule that
// makes the scale tool's workers sample privately.
type queryCounter struct {
	mu          sync.Mutex
	statements  int
	deviceReads int
	maxVars     int
	log         []string
}

func registerQueryCounter(db *gorm.DB, c *queryCounter) {
	db.Callback().Query().After("gorm:find").Register("ourway:scale:count", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		vars := len(tx.Statement.Vars)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.statements++
		if strings.Contains(sql, "FROM devices") {
			c.deviceReads++
		}
		if vars > c.maxVars {
			c.maxVars = vars
		}
		c.log = append(c.log, fmt.Sprintf("%d vars :: %s", vars, sql))
	})
}

type countSnapshot struct {
	statements  int
	deviceReads int
	maxVars     int
}

func (c *queryCounter) snapshot() countSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return countSnapshot{statements: c.statements, deviceReads: c.deviceReads, maxVars: c.maxVars}
}

// reset clears the counters, so a test measures one request and not the
// fixture's own seeding.
func (c *queryCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = 0
	c.deviceReads = 0
	c.maxVars = 0
	c.log = nil
}

func (c *queryCounter) history() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.log...)
}

type scaleFixture struct {
	baseURL string
	st      *store.Store
	count   *queryCounter
}

func newScaleFixture(t *testing.T) *scaleFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory SQLite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	counter := &queryCounter{}
	registerQueryCounter(db, counter)

	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub("http://localhost:3000", "")
	engine := alerts.NewEngine(st.Alerts)
	// The router's context is cancellable and is cancelled **before** the
	// database closes. `SetupRouter` starts background jobs on it (webhook retry,
	// the patch scanner, the policy engine), so a fixture that hands them
	// `context.Background()` leaves them polling the store for the rest of the
	// process: the log fills with `sql: database is closed`, and every statement
	// they issue lands in the counter this test is measuring. The measurement is
	// of a request, so the fixture stops everything that is not the request.
	ctx, cancel := context.WithCancel(context.Background())
	router := SetupRouter(ctx, st, jwtAuth, hub, engine, "http://localhost:3000", "", 100, 0)
	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		cancel()
		// A short grace lets a tick already in flight finish against a database
		// that is still open.
		time.Sleep(150 * time.Millisecond)
		ts.Close()
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return &scaleFixture{baseURL: ts.URL, st: st, count: counter}
}

// makeScaleManager creates a manager granted exactly `clientIDs`, and logs them
// in for a real token (2.3-1's fixture rule: users come from the store, because
// the first registered account bootstraps admin).
func (f *scaleFixture) makeScaleManager(t *testing.T, username string, clientIDs ...string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("Passw0rd-2.3!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := &models.User{ID: uuid.NewString(), Username: username, Email: username + "@example.test", PasswordHash: string(hash), Provider: "local"}
	if err := f.st.Users.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role, err := f.st.Roles.GetRoleByName("manager")
	if err != nil {
		t.Fatalf("role: %v", err)
	}
	if err := f.st.UserRoles.AssignRole(user.ID, role.ID); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	for _, id := range clientIDs {
		if err := f.st.Clients.AddUserClient(user.ID, id); err != nil {
			t.Fatalf("grant client: %v", err)
		}
	}
	return loginUser(t, f.baseURL, username, "Passw0rd-2.3!")
}

// seedTenant creates a client with `units` units and three unresolved alerts on
// each, and returns the client id.
func (f *scaleFixture) seedTenant(t *testing.T, name string, units int) string {
	t.Helper()
	client, err := f.st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: name, Slug: "slug-" + name})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	for i := 0; i < units; i++ {
		label := fmt.Sprintf("%s-%04d", name, i)
		device := &models.Device{
			ID: uuid.NewString(), Name: label, Hostname: label, OS: "linux", Arch: "amd64",
			Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(), ClientID: &client.ID,
		}
		if err := f.st.Devices.Create(device); err != nil {
			t.Fatalf("create device: %v", err)
		}
		for s := 0; s < 3; s++ {
			alert := &models.Alert{
				ID: uuid.NewString(), DeviceID: device.ID, DeviceName: label,
				Severity: "warning", Message: fmt.Sprintf("%s signal %d", label, s), Metric: "disk",
				Value: 91, Threshold: 90,
			}
			if err := f.st.Alerts.Create(alert); err != nil {
				t.Fatalf("create alert: %v", err)
			}
		}
	}
	return client.ID
}

func (f *scaleFixture) get(t *testing.T, path, token string) int {
	t.Helper()
	status, _, _ := f.do(t, http.MethodGet, path, token, nil)
	return status
}

func (f *scaleFixture) do(t *testing.T, method, path, token string, body any) (int, []byte, string) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, f.baseURL+path, payload)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	payloadOut, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, payloadOut, resp.Header.Get("X-OurWay-Client-Scope")
}

// cheapest runs `fn` `runs` times on a cleared counter and returns the smallest
// values seen. Statements are counted on the store the whole fixture shares, and
// the server's own background jobs (the sessions gateway, the policy engine, the
// webhook retry loop) issue them too: a tick landing inside one window and not
// the next is not a property of the request, and extra statements only ever
// *add* to a window. The minimum is therefore the request's own cost; a
// comparison of maxima compares a request with a tick, which is how a
// measurement of a constant shape reads as a flake.
func (f *scaleFixture) cheapest(t *testing.T, runs int, fn func()) countSnapshot {
	t.Helper()
	best := countSnapshot{statements: 1 << 30, deviceReads: 1 << 30, maxVars: 1 << 30}
	for i := 0; i < runs; i++ {
		f.count.reset()
		fn()
		s := f.count.snapshot()
		if s.statements < best.statements {
			best.statements = s.statements
		}
		if s.deviceReads < best.deviceReads {
			best.deviceReads = s.deviceReads
		}
		if s.maxVars < best.maxVars {
			best.maxVars = s.maxVars
		}
	}
	return best
}

// TestScopedListQueryCountIsConstant is the acceptance criterion "the request
// count is unchanged from 2.2", stated as a measurement: the statements a
// tenant's alert list costs must not depend on how many units that tenant has.
func TestScopedListQueryCountIsConstant(t *testing.T) {
	f := newScaleFixture(t)

	small := f.seedTenant(t, "small", 20)
	big := f.seedTenant(t, "big", 400)

	smallToken := f.makeScaleManager(t, "scale-small", small)
	bigToken := f.makeScaleManager(t, "scale-big", big)

	// Each request is measured as the cheapest of three: the fixture's own
	// seeding is already in the counter, and the server's background jobs add
	// statements to some windows and not others.
	smallReads := f.cheapest(t, 3, func() {
		if code := f.get(t, "/api/alerts", smallToken); code != http.StatusOK {
			t.Fatalf("small alerts = %d", code)
		}
	})
	bigReads := f.cheapest(t, 3, func() {
		if code := f.get(t, "/api/alerts", bigToken); code != http.StatusOK {
			t.Fatalf("big alerts = %d", code)
		}
	})

	// 20 units × 3 alerts vs 400 units × 3 alerts: same statements. If scoping
	// ever resolves a unit per row, `bigReads` jumps and this fails.
	if bigReads.statements != smallReads.statements {
		t.Fatalf("scoped alert list cost %d statements for a 400-unit tenant, %d for a 20-unit tenant: %s",
			bigReads.statements, smallReads.statements, strings.Join(f.count.history(), "\n"))
	}
}

// TestScopedListQueryIsOverTheClientSet is the shape rule: a scoped list is a
// statement over the caller's **clients**, so its bind-parameter count is the
// tenant count plus a constant — never the unit count. The 2.3-2 shape
// (`device_id IN (…units…)`) fails this test at 400 units.
func TestScopedListQueryIsOverTheClientSet(t *testing.T) {
	f := newScaleFixture(t)
	clientID := f.seedTenant(t, "wide", 400)
	token := f.makeScaleManager(t, "scale-wide", clientID)

	wide := f.cheapest(t, 3, func() {
		if code := f.get(t, "/api/alerts", token); code != http.StatusOK {
			t.Fatalf("alerts = %d", code)
		}
	})
	// A client grant, a scope header, and the constants of the statement itself.
	// Anything proportional to 400 units means the units went into the SQL.
	if wide.maxVars > 8 {
		t.Fatalf("scoped alert list carried %d bind parameters for a 400-unit tenant: %s",
			wide.maxVars, strings.Join(f.count.history(), "\n"))
	}
}

// TestBulkTargetChecksCostOneFleetRead is 2.3-8's named case: a bulk command's
// scope checks are per target, and each check must not re-read the caller's
// fleet. The count of fleet reads is asserted for one target and for a full
// panel of them.
func TestBulkTargetChecksCostOneFleetRead(t *testing.T) {
	f := newScaleFixture(t)
	clientID := f.seedTenant(t, "bulk", 200)
	token := f.makeScaleManager(t, "scale-bulk", clientID)

	devices, err := f.st.Devices.ListScoped([]string{clientID}, false)
	if err != nil || len(devices) != 200 {
		t.Fatalf("fixture units = %d (err %v)", len(devices), err)
	}
	all := make([]string, 0, len(devices))
	for _, d := range devices {
		all = append(all, d.ID)
	}

	one := func(ids []string) int {
		return f.cheapest(t, 3, func() {
			code, _, _ := f.do(t, http.MethodPost, "/api/devices/tags/bulk", token, map[string]any{
				"device_ids": ids, "add": []string{"scoped"}, "remove": []string{},
			})
			if code != http.StatusOK {
				t.Fatalf("bulk tags = %d", code)
			}
		}).deviceReads
	}

	oneRead := one(all[:1])
	fullRead := one(all)

	// A bulk command over 200 targets resolving the caller's fleet once is a
	// constant; per-target resolution is 200.
	if oneRead > 2 || fullRead > 2 {
		t.Fatalf("bulk target checks cost %d fleet reads for 1 target and %d for 200 (want a constant ≤ 2): %s",
			oneRead, fullRead, strings.Join(f.count.history(), "\n"))
	}
}
