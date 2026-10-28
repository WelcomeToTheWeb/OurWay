//go:build loadtest

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"
)

// 2.3-8 — the console read at tenant scale.
//
// The scale tool's job in 2.0 was the agent edge: 10,000 devices heartbeating.
// Tenancy moved the risk to the console edge, where the shape of a read changed
// and nothing in the agent path notices: a scoped technician's fleet list must
// cost the same as an admin's list of the same page, not one query per unit.
//
// So the tool now drives both. Phase 3 seeds the tenancy a real installation
// has after the upgrade guide is followed — tenants, unit assignments, a
// technician granted one tenant — and Phase 4 runs the same console campaign as
// the admin (the unscoped baseline the plan names) and as the granted
// technician (the scoped measurement), on the endpoints a shift actually
// reads: the fleet sheet, the alert ledger, the patch overview.
//
// The verdict is the plan's criterion, stated as a comparison: the scoped
// p95 within 10% of the unscoped p95, and zero failed requests.

// TenancyConfig configures the tenancy seeding and the console campaign.
type TenancyConfig struct {
	Tenants        int
	ConsoleRPS     int
	ConsoleSeconds int
	ConsoleWorkers int
	AdminPassword  string
}

type endpointStat struct {
	name     string
	requests int64
	failures int64
	latency  []time.Duration
}

func (e *endpointStat) p95() time.Duration { return e.percentile(95) }

func (e *endpointStat) p50() time.Duration { return e.percentile(50) }

func (e *endpointStat) percentile(p int) time.Duration {
	if len(e.latency) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), e.latency...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted) - 1) * p / 100
	return sorted[idx]
}

type campaignResult struct {
	actor     string
	units     int
	endpoints []*endpointStat
}

// apiCall issues one authenticated console request and returns its status.
func apiCall(client *http.Client, url, token string) int {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func postJSON(client *http.Client, url, token string, body any) (int, []byte) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func putJSON(client *http.Client, url, token string, body any) int {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// signIn signs a user in, registering the account first when it does not
// exist. Registration is how a fresh installation gets its first account — and
// the first account is the admin (2.3's fixtures say why a tool must know
// that) — but registration answers the user row, not a token, so every caller
// of it follows with a login.
// createUser makes an operator account the way a real installation does: the
// admin creates it. Self-service registration closes after the first account —
// the bootstrap is an installation's opening act, not a door — so a tool that
// wants technicians must go through the admin. Returns the account id, which is
// what a client grant is made of.
func createUser(client *http.Client, cfg ScaleConfig, tc TenancyConfig, adminToken, username, role string) string {
	code, body := postJSON(client, cfg.ServerURL+"/api/users", adminToken, map[string]any{
		"username": username,
		"email":    username + "@example.test",
		"password": tc.AdminPassword,
		"roles":    []string{role},
	})
	if code != http.StatusCreated && code != http.StatusOK && code != http.StatusConflict {
		log.Fatalf("create %s: %d %s", username, code, body)
	}
	var env struct {
		ID   string `json:"id"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	json.Unmarshal(body, &env)
	if env.ID != "" {
		return env.ID
	}
	if env.User.ID != "" {
		return env.User.ID
	}
	// Created without an id in the body: read it back from the user list, the
	// way the console's own fixtures do.
	code, body = apiGet(client, cfg.ServerURL+"/api/users", adminToken)
	if code != http.StatusOK {
		log.Fatalf("user list = %d", code)
	}
	var list struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	}
	json.Unmarshal(body, &list)
	for _, u := range list.Users {
		if u.Username == username {
			return u.ID
		}
	}
	log.Fatalf("created %s, and it is not in the user list", username)
	return ""
}

// signIn signs an account in. For the installation's first account it
// registers first, because registration is how a fresh install is bootstrapped
// and the first account is the admin (2.3's fixture rule says why a tool must
// know that); registration answers the user row and no token, so it is
// followed by a login.
func signIn(client *http.Client, cfg ScaleConfig, tc TenancyConfig, username string) string {
	code, body := postJSON(client, cfg.ServerURL+"/api/auth/login", "", map[string]any{
		"username": username, "password": tc.AdminPassword,
	})
	if code == http.StatusOK {
		var env struct {
			AccessToken string `json:"access_token"`
		}
		json.Unmarshal(body, &env)
		return env.AccessToken
	}
	code, body = postJSON(client, cfg.ServerURL+"/api/auth/register", "", map[string]any{
		"username": username, "email": username + "@example.test", "password": tc.AdminPassword,
	})
	if code != http.StatusCreated && code != http.StatusOK {
		log.Fatalf("register %s: %d %s", username, code, body)
	}
	code, body = postJSON(client, cfg.ServerURL+"/api/auth/login", "", map[string]any{
		"username": username, "password": tc.AdminPassword,
	})
	if code != http.StatusOK {
		log.Fatalf("login %s after register: %d %s", username, code, body)
	}
	var env struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &env)
	return env.AccessToken
}

func loginAdmin(client *http.Client, cfg ScaleConfig, tc TenancyConfig) string {
	return signIn(client, cfg, tc, "scale-admin")
}

func seedTenancy(client *http.Client, cfg ScaleConfig, tc TenancyConfig, adminToken string) (techTokens []string, scopedUnits int) {
	code, body := apiGet(client, cfg.ServerURL+"/api/devices", adminToken)
	if code != http.StatusOK {
		log.Fatalf("device list for seeding = %d", code)
	}
	var fleet struct {
		Devices []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &fleet); err != nil {
		log.Fatalf("decode fleet: %v", err)
	}
	if len(fleet.Devices) == 0 {
		log.Fatal("no devices to assign")
	}

	perTenant := len(fleet.Devices) / tc.Tenants
	ids := make([]string, 0, len(fleet.Devices))
	for _, d := range fleet.Devices {
		ids = append(ids, d.ID)
	}
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

	// The technician's tenant is the one with `perTenant` units, so the
	// criterion's "a technician scoped to 2,000 of 10,000" is the measured case,
	// not an average over the whole installation.
	scopedIDs := ids[:perTenant]
	for t := 0; t < tc.Tenants; t++ {
		start := t * perTenant
		end := start + perTenant
		if t == tc.Tenants-1 {
			end = len(ids)
		}
		name := fmt.Sprintf("Scale Tenant %d", t+1)
		code, body := postJSON(client, cfg.ServerURL+"/api/clients", adminToken, map[string]any{"name": name})
		if code == http.StatusConflict {
			// A rerun against an installation that is already seeded. The slug
			// is the tenant's identity, so the tool resolves the existing one
			// and re-assigns, instead of failing a measurement it can make.
			fmt.Printf("  Tenant %q already exists; reusing it\n", name)
		} else if code != http.StatusCreated && code != http.StatusOK {
			log.Fatalf("create tenant %d: %d %s", t, code, body)
		}
		var env struct {
			Client struct {
				ID string `json:"id"`
			} `json:"client"`
		}
		json.Unmarshal(body, &env)
		if env.Client.ID == "" {
			env.Client.ID = findTenantIDByName(client, cfg, adminToken, name)
		}
		if code := putJSON(client, cfg.ServerURL+"/api/clients/"+env.Client.ID+"/devices", adminToken, map[string]any{
			"device_ids": ids[start:end],
		}); code != http.StatusOK {
			log.Fatalf("assign tenant %d (%d units): %d", t, end-start, code)
		}
		fmt.Printf("  Tenant %d owns %d units\n", t+1, end-start)
	}

	// The technicians: one per tenant, each granted exactly that tenant. The
	// campaign runs over all of them, because tenancy as an MSP installs it is
	// several operators each on their own slice — and one account issuing 100
	// req/s is above the server's per-user budget (2.2-3) by construction,
	// which the script raises and this comment names.
	//
	// The grant is a row, not a claim (2.3-1): this is the operation that makes
	// each technician's fleet, and it takes effect on their next request.
	tenantIDs := listTenantIDs(client, cfg, adminToken)
	if len(tenantIDs) < tc.Tenants {
		log.Fatalf("seeded %d tenants, the list answers %d", tc.Tenants, len(tenantIDs))
	}
	tokens := make([]string, 0, tc.Tenants)
	for t := 0; t < tc.Tenants; t++ {
		userID := createUser(client, cfg, tc, adminToken, fmt.Sprintf("scale-tech-%d", t+1), "manager")
		token := signIn(client, cfg, tc, fmt.Sprintf("scale-tech-%d", t+1))
		if code := putJSON(client, cfg.ServerURL+"/api/clients/"+tenantIDs[t]+"/users", adminToken, map[string]any{
			"user_ids": []string{userID},
		}); code != http.StatusOK {
			log.Fatalf("grant tenant %d: %d", t, code)
		}
		tokens = append(tokens, token)
	}
	_ = scopedIDs
	return tokens, perTenant
}

func findTenantIDByName(client *http.Client, cfg ScaleConfig, adminToken, name string) string {
	code, body := apiGet(client, cfg.ServerURL+"/api/clients", adminToken)
	if code != http.StatusOK {
		log.Fatalf("client list = %d", code)
	}
	var env struct {
		Clients []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"clients"`
	}
	json.Unmarshal(body, &env)
	for _, c := range env.Clients {
		if c.Name == name {
			return c.ID
		}
	}
	log.Fatalf("tenant %q is not in the client list", name)
	return ""
}

func listTenantIDs(client *http.Client, cfg ScaleConfig, adminToken string) []string {
	code, body := apiGet(client, cfg.ServerURL+"/api/clients", adminToken)
	if code != http.StatusOK {
		log.Fatalf("client list = %d", code)
	}
	var env struct {
		Clients []struct {
			ID string `json:"id"`
		} `json:"clients"`
	}
	json.Unmarshal(body, &env)
	ids := make([]string, 0, len(env.Clients))
	for _, c := range env.Clients {
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		log.Fatal("no tenants to grant")
	}
	return ids
}

func apiGet(client *http.Client, url, token string) (int, []byte) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// runCampaign drives one actor's console reads for tc.ConsoleSeconds at
// tc.ConsoleRPS requests per second, spread over tc.ConsoleWorkers workers.
// runCampaign drives the actors' console reads for tc.ConsoleSeconds at
// tc.ConsoleRPS requests per second in total. Each worker sticks to one account
// for its lifetime — a bearer token is a session, and bouncing between accounts
// per request would measure the token cache, not the read.
func runCampaign(client *http.Client, cfg ScaleConfig, tc TenancyConfig, actor string, tokens []string, units int) *campaignResult {
	endpoints := []string{"/api/devices", "/api/alerts", "/api/patch/overview"}
	stats := make([]*endpointStat, 0, len(endpoints))
	for _, name := range endpoints {
		stats = append(stats, &endpointStat{name: name})
	}
	byName := map[string]*endpointStat{}
	for _, s := range stats {
		byName[s.name] = s
	}

	// A broadcast deadline, not a `time.After` channel: that channel carries
	// **one** value, and the first worker to take it leaves the rest parked in
	// select forever — a load tool that never finishes its own campaign.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(tc.ConsoleSeconds)*time.Second)
	defer cancel()
	interval := time.Duration(float64(time.Second) / float64(tc.ConsoleRPS) * float64(tc.ConsoleWorkers))

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Each worker keeps its own samples and merges them under a mutex. Sharing
	// the slices and counters across goroutines is a data race, and a load
	// tool whose numbers come from a race is not a measurement.
	for w := 0; w < tc.ConsoleWorkers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			token := tokens[w%len(tokens)]
			own := map[string]*endpointStat{}
			for _, s := range stats {
				own[s.name] = &endpointStat{name: s.name}
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			i := w
			for {
				select {
				case <-ticker.C:
					name := endpoints[i%len(endpoints)]
					i++
					start := time.Now()
					code := apiCall(client, cfg.ServerURL+name, token)
					stat := own[name]
					stat.latency = append(stat.latency, time.Since(start))
					stat.requests++
					if code < 200 || code >= 300 {
						stat.failures++
					}
				case <-ctx.Done():
					mu.Lock()
					for _, s := range stats {
						s.requests += own[s.name].requests
						s.failures += own[s.name].failures
						s.latency = append(s.latency, own[s.name].latency...)
					}
					mu.Unlock()
					return
				}
			}
		}(w)
	}
	wg.Wait()

	total := int64(0)
	failures := int64(0)
	for _, s := range stats {
		total += s.requests
		failures += s.failures
	}
	fmt.Printf("  %s: %d requests over %d account(s), %d failures, %d units per tenant\n", actor, total, len(tokens), failures, units)
	for _, s := range stats {
		fmt.Printf("    %-20s %5d req  %d fail  p50 %8s  p95 %8s\n", s.name, s.requests, s.failures, s.p50(), s.p95())
	}
	return &campaignResult{actor: actor, units: units, endpoints: stats}
}

// reportTenancy prints the plan's verdict: scoped p95 within 10% of the
// unscoped baseline, and zero failures.
func reportTenancy(baseline, scoped *campaignResult) bool {
	ok := true
	fmt.Printf("\n=== Tenancy read verdict (scoped technician vs unscoped baseline) ===\n")
	for _, s := range scoped.endpoints {
		var base *endpointStat
		for _, b := range baseline.endpoints {
			if b.name == s.name {
				base = b
			}
		}
		if base == nil || len(base.latency) == 0 {
			continue
		}
		budget := time.Duration(float64(base.p95()) * 1.1)
		verdict := "PASS"
		if s.p95() > budget {
			verdict = "FAIL"
			ok = false
		}
		if s.failures > 0 {
			verdict = "FAIL"
			ok = false
		}
		fmt.Printf("  %-20s scoped p95 %8s vs baseline %8s (budget %8s) -> %s\n", s.name, s.p95(), base.p95(), budget, verdict)
	}
	return ok
}
