package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"ourway/server/models"
)

// 2.3-0 — the tenant axis, tested where it is defined: in the store.
//
// The shape these tests insist on is the one the release turns on: **unassigned
// is `devices.client_id IS NULL`**. There is no unassigned row to create,
// rename, collide with, or delete, and the scoped reads answer from that one
// definition. A store that returned the fleet to a caller with no client rows
// would pass every test written against a "filter later in the handler" design,
// which is the design this release replaces.

func newClientStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewWithDB(newMigrateDB(t))
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	return st
}

func mkDevice(t *testing.T, st *Store, name, clientID string) *models.Device {
	t.Helper()
	d := &models.Device{
		ID:       uuid.NewString(),
		Name:     name,
		Hostname: name,
		OS:       "linux",
		Arch:     "amd64",
		Status:   "online",
		Tags:     []string{},
		// DeviceKey carries a unique index, and an empty key is one value: a
		// fixture that omits it fails on the second device with "unique
		// constraint", which reads like a schema bug and is a fixture bug.
		DeviceKey: uuid.NewString(),
	}
	if clientID != "" {
		d.ClientID = &clientID
	}
	if err := st.Devices.Create(d); err != nil {
		t.Fatalf("create device %s: %v", name, err)
	}
	return d
}

func TestClientStoreSlugIsNormalizedAndUnique(t *testing.T) {
	st := newClientStore(t)

	created, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Acme Widgets", Slug: "  Acme Widgets "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Slug != "acme-widgets" {
		t.Fatalf("slug = %q, want the normalized %q", created.Slug, "acme-widgets")
	}

	// "Acme  Widgets" and "acme-widgets" are the same client: the collision is
	// reported, not silently given a second slug.
	if _, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Other", Slug: "Acme  Widgets"}); err == nil {
		t.Fatal("second client with a colliding slug was accepted")
	} else if !isSlugTaken(err) {
		t.Fatalf("collision error = %v, want the slug-taken sentinel", err)
	}

	// A name with nothing legal in it normalizes to nothing and is refused —
	// inventing a slug is how two clients end up sharing one.
	if _, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "!!!", Slug: "!!!"}); err == nil {
		t.Fatal("a slug that normalizes to empty was accepted")
	}

	// Renaming onto another client's slug is refused; renaming to a free slug
	// is not.
	globex, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Globex", Slug: "globex"})
	if err != nil {
		t.Fatalf("Create globex: %v", err)
	}
	created.Name = "Globex"
	created.Slug = "globex"
	if err := st.Clients.Update(created); err == nil || !isSlugTaken(err) {
		t.Fatalf("update onto a taken slug = %v, want the slug-taken sentinel", err)
	}
	if created.ID == globex.ID {
		t.Fatal("the two clients share an id — Create minted one for both")
	}
	created.Slug = "acme-renamed"
	if err := st.Clients.Update(created); err != nil {
		t.Fatalf("update to a free slug: %v", err)
	}
	if got, _ := st.Clients.GetBySlug("Acme-Renamed"); got == nil || got.ID != created.ID {
		t.Fatal("GetBySlug must normalize its input the same way Create does")
	}
}

// The upgrade acceptance, stated as a store test: a device that existed before
// clients existed is *the same row* afterwards, and reads as unassigned.
func TestClientStoreExistingDevicesSurviveAsUnassigned(t *testing.T) {
	st := newClientStore(t)

	d := mkDevice(t, st, "pre-2.3-unit", "")

	after, err := st.Devices.GetByID(d.ID)
	if err != nil {
		t.Fatalf("GetByID after migration: %v", err)
	}
	if after.ClientID != nil {
		t.Fatalf("pre-2.3 device has client_id %v; upgrade must leave it NULL", *after.ClientID)
	}

	// The unassigned bucket is queryable — that is how `GET /api/clients`
	// presents it without a row.
	unassigned, err := st.Devices.ListScoped(nil, true)
	if err != nil {
		t.Fatalf("ListScoped(unassigned): %v", err)
	}
	if len(unassigned) != 1 || unassigned[0].ID != d.ID {
		t.Fatalf("unassigned bucket = %+v, want exactly the pre-2.3 device", unassigned)
	}

	// The sentinel is a name, never a stored value: nothing here writes it.
	if got, _ := st.Devices.GetByID(models.UnassignedClientID); got != nil {
		t.Fatal("the unassigned sentinel resolved to a device row")
	}
}

func TestClientStoreScopedListsAreTheQueryNotTheFilter(t *testing.T) {
	st := newClientStore(t)

	a, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha", Slug: "alpha"})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	b, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Beta", Slug: "beta"})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}
	mkDevice(t, st, "a1", a.ID)
	mkDevice(t, st, "a2", a.ID)
	bDevice := mkDevice(t, st, "b1", b.ID)
	mkDevice(t, st, "free", "")

	// Scoped to Alpha: Alpha's units, and nothing else.
	inA, err := st.Devices.ListScoped([]string{a.ID}, false)
	if err != nil {
		t.Fatalf("ListScoped(alpha): %v", err)
	}
	if len(inA) != 2 {
		t.Fatalf("scope alpha returned %d devices, want 2", len(inA))
	}
	for _, d := range inA {
		if d.ClientID == nil || *d.ClientID != a.ID {
			t.Fatalf("scope alpha leaked device %s (client %v)", d.Name, d.ClientID)
		}
	}

	// Scoped to Alpha, including unassigned: Alpha + the free unit, never Beta's.
	withFree, err := st.Devices.ListScoped([]string{a.ID}, true)
	if err != nil {
		t.Fatalf("ListScoped(alpha, unassigned): %v", err)
	}
	for _, d := range withFree {
		if d.ID == bDevice.ID {
			t.Fatal("a scope of {alpha, unassigned} returned a beta device")
		}
	}
	if len(withFree) != 3 {
		t.Fatalf("scope {alpha, unassigned} = %d devices, want 3", len(withFree))
	}

	// No clients, no unassigned: no devices. This is the case a permissive
	// implementation turns into "sees everything".
	none, err := st.Devices.ListScoped(nil, false)
	if err != nil {
		t.Fatalf("ListScoped(empty scope): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("an empty scope saw %d devices, want 0", len(none))
	}
}

// Deleting a client is a bookkeeping act, not a destructive one.
func TestClientStoreDeleteMovesDevicesToUnassigned(t *testing.T) {
	st := newClientStore(t)

	a, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha", Slug: "alpha"})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	d := mkDevice(t, st, "a1", a.ID)
	uid := uuid.NewString()
	if err := st.Clients.AddUserClient(uid, a.ID); err != nil {
		t.Fatalf("AddUserClient: %v", err)
	}

	if err := st.Clients.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got, _ := st.Clients.GetByID(a.ID); got != nil {
		t.Fatal("client row survived Delete")
	}
	survived, err := st.Devices.GetByID(d.ID)
	if err != nil {
		t.Fatalf("device vanished with its client: %v", err)
	}
	if survived.ClientID != nil {
		t.Fatalf("device kept client_id %v after its client was deleted; want NULL", *survived.ClientID)
	}
	// The scope rows referencing a client that no longer exists are gone: a
	// stale grant is a scope nobody can see and a support ticket nobody can
	// close.
	if ids, _ := st.Clients.ClientIDsForUser(uid); len(ids) != 0 {
		t.Fatalf("user scope kept %v after the client was deleted", ids)
	}
}

func TestClientStoreAssignAndUnassign(t *testing.T) {
	st := newClientStore(t)

	a, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha", Slug: "alpha"})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	b, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Beta", Slug: "beta"})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}
	d := mkDevice(t, st, "d1", b.ID)

	affected, err := st.Clients.AssignDevices(a.ID, []string{d.ID})
	if err != nil {
		t.Fatalf("AssignDevices: %v", err)
	}
	if affected != 1 {
		t.Fatalf("AssignDevices affected %d, want 1", affected)
	}
	got, _ := st.Devices.GetByID(d.ID)
	if got.ClientID == nil || *got.ClientID != a.ID {
		t.Fatalf("device client = %v, want alpha", got.ClientID)
	}

	// Unassign is the same call with no client — one definition of ownership,
	// so assign and unassign cannot drift apart.
	if _, err := st.Clients.AssignDevices("", []string{d.ID}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	got, _ = st.Devices.GetByID(d.ID)
	if got.ClientID != nil {
		t.Fatalf("device client = %v after unassign, want NULL", *got.ClientID)
	}
}

func TestClientStoreUserScopeIsRows(t *testing.T) {
	st := newClientStore(t)

	a, _ := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha", Slug: "alpha"})
	b, _ := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Beta", Slug: "beta"})
	uid := uuid.NewString()

	if ids, _ := st.Clients.ClientIDsForUser(uid); len(ids) != 0 {
		t.Fatalf("a user with no grants has scope %v, want none", ids)
	}
	if err := st.Clients.AddUserClient(uid, a.ID); err != nil {
		t.Fatalf("AddUserClient alpha: %v", err)
	}
	// Idempotent: granting twice is not a second scope.
	if err := st.Clients.AddUserClient(uid, a.ID); err != nil {
		t.Fatalf("AddUserClient alpha (repeat): %v", err)
	}
	if err := st.Clients.AddUserClient(uid, b.ID); err != nil {
		t.Fatalf("AddUserClient beta: %v", err)
	}
	ids, _ := st.Clients.ClientIDsForUser(uid)
	if len(ids) != 2 {
		t.Fatalf("user scope = %v, want two clients", ids)
	}
	if err := st.Clients.AddUserClient(uid, uuid.NewString()); err == nil {
		t.Fatal("a grant naming a client that does not exist was accepted")
	}

	// Revocation is a row removed, effective on the next read — the reason
	// scope is not a token claim (2.3-1).
	if err := st.Clients.RemoveUserClient(uid, b.ID); err != nil {
		t.Fatalf("RemoveUserClient: %v", err)
	}
	ids, _ = st.Clients.ClientIDsForUser(uid)
	if len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("after revocation, scope = %v, want exactly alpha", ids)
	}

	clients, _ := st.Clients.ListForUsers([]string{uid})
	if len(clients) != 1 || clients[0].ID != a.ID {
		t.Fatalf("ListForUsers = %+v, want exactly alpha", clients)
	}
}

func TestClientStoreBulkTargetCheckAnswersInScope(t *testing.T) {
	st := newClientStore(t)

	a, _ := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha", Slug: "alpha"})
	b, _ := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Beta", Slug: "beta"})
	inA := mkDevice(t, st, "a1", a.ID)
	inB := mkDevice(t, st, "b1", b.ID)
	free := mkDevice(t, st, "free", "")

	// The bulk route's question (2.3-3): of these targets, which are in my
	// scope? Answered by the store, before anything is written.
	ids, err := st.Clients.DeviceIDsIn([]string{a.ID}, false, []string{inA.ID, inB.ID, free.ID})
	if err != nil {
		t.Fatalf("DeviceIDsIn: %v", err)
	}
	if len(ids) != 1 || ids[0] != inA.ID {
		t.Fatalf("DeviceIDsIn = %v, want exactly the alpha device", ids)
	}

	ids, err = st.Clients.DeviceIDsIn([]string{a.ID}, true, []string{inA.ID, inB.ID, free.ID})
	if err != nil {
		t.Fatalf("DeviceIDsIn(unassigned): %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("DeviceIDsIn(alpha, unassigned) = %v, want the alpha and the free unit", ids)
	}
}

// isSlugTaken asserts the sentinel, not the message text: the API renders this
// error as 409, so the store has to be checkable by identity.
func isSlugTaken(err error) bool {
	return errors.Is(err, ErrSlugTaken)
}
