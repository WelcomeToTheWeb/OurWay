package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ourway/server/models"
)

// ErrSlugTaken is the store's answer to a client slug that already exists.
// The API renders it as 409; the console shows it against the name field.
var ErrSlugTaken = errors.New("client slug already taken")

// ClientStoreInterface is the tenant store (2.3-0): the clients themselves, and
// the client scope of a user.
//
// Two rules are baked into these signatures because they are the rules the
// release turns on:
//
//   - **"Unassigned" is `devices.client_id IS NULL`.** Nothing here stores a
//     row for it, and nothing here returns a fake client. The API presents the
//     sentinel, this layer answers in NULLs.
//   - **A user's scope is rows, not a token claim.** Scope changes take effect
//     on the caller's next request, so the resolver reads these rows every time.
type ClientStoreInterface interface {
	// Create inserts a client, filling its id and normalizing its slug, and returns
	// the stored row: the console needs the canonical slug it will filter on,
	// not the string the operator typed.
	Create(c *models.Client) (*models.Client, error)
	GetByID(id string) (*models.Client, error)
	GetBySlug(slug string) (*models.Client, error)
	// List returns every client, plus the count of devices that carry no
	// client — so a ledger can show the unassigned bucket without inventing a
	// row for it.
	List() ([]models.Client, error)
	// ListForUsers returns the clients named by `userIDs`, i.e. the clients a
	// caller's scope names. Admins do not come through here (2.3-1).
	ListForUsers(userIDs []string) ([]models.Client, error)
	Update(c *models.Client) error
	// Delete removes a client and moves its devices to unassigned
	// (`client_id = NULL`). It never deletes a device: losing a tenant must
	// not lose its fleet.
	Delete(id string) error

	// AssignDevices moves devices to a client. `clientID == ""` is the
	// unassign direction, kept as one call so "assign" and "unassign" cannot
	// drift into two different definitions of ownership.
	AssignDevices(clientID string, deviceIDs []string) (int64, error)
	// DeviceIDsIn returns the devices of `deviceIDs` that are in the client
	// set — the store-level check a bulk route needs before it touches
	// anything (2.3-3). `includeUnassigned` is what makes "unassigned"
	// queryable like any other bucket.
	DeviceIDsIn(clientIDs []string, includeUnassigned bool, deviceIDs []string) ([]string, error)
	// DeviceIDsNotIn is the complement — the bulk route's "name the targets
	// this caller may not touch", so a partial bulk fails as 400 naming ids
	// instead of writing to the ones it may.
	DeviceIDsNotIn(clientIDs []string, includeUnassigned bool, deviceIDs []string) ([]string, error)
	// UsersForClient lists the accounts granted this client.
	UsersForClient(clientID string) ([]models.User, error)
	// GrantsCount is every client's grant count in one grouped read: the
	// per-client ledger prints "technicians" for every tenant, and a ledger of
	// 40 tenants must not cost 40 queries to render (2.3-5).
	GrantsCount() (map[string]int, error)

	// Client scope of a user.
	ClientIDsForUser(userID string) ([]string, error)
	AddUserClient(userID, clientID string) error
	RemoveUserClient(userID, clientID string) error
}

// ClientStore is the ClientStoreInterface over Postgres/SQLite via gorm.
type ClientStore struct {
	db *gorm.DB
}

// Create inserts a client. The slug is normalized here, on the way in, so two
// callers who typed "Acme " and "ACME" cannot both succeed.
func (s *ClientStore) Create(c *models.Client) (*models.Client, error) {
	c.Slug = models.NormalizeSlug(c.Slug)
	if c.Slug == "" {
		return nil, errors.New("client slug is empty after normalization")
	}
	if c.ID == "" {
		c.ID = models.NewClientID()
	}
	if taken, _ := s.GetBySlug(c.Slug); taken != nil {
		return nil, fmt.Errorf("%w: %s", ErrSlugTaken, c.Slug)
	}
	if err := s.db.Create(c).Error; err != nil {
		return nil, err
	}
	return c, nil
}

// GetByID fetches a client by UUID. Returns nil when absent (no error), the
// shape the API needs to answer "no such client" with 404.
func (s *ClientStore) GetByID(id string) (*models.Client, error) {
	var c models.Client
	if err := s.db.First(&c, "id = ?", id).Error; err != nil {
		return nil, nil
	}
	return &c, nil
}

// GetBySlug fetches a client by its normalized slug. A nil client means "no
// such client", the shape the enrollment and console routes need in order to
// answer 404 without mapping a gorm error.
func (s *ClientStore) GetBySlug(slug string) (*models.Client, error) {
	var c models.Client
	if err := s.db.First(&c, "slug = ?", models.NormalizeSlug(slug)).Error; err != nil {
		return nil, nil
	}
	return &c, nil
}

func (s *ClientStore) List() ([]models.Client, error) {
	var clients []models.Client
	if err := s.db.Order("name ASC").Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

func (s *ClientStore) ListForUsers(userIDs []string) ([]models.Client, error) {
	if len(userIDs) == 0 {
		return []models.Client{}, nil
	}
	var clientIDs []string
	if err := s.db.Model(&models.UserClient{}).
		Where("user_id IN ?", userIDs).
		Distinct().
		Pluck("client_id", &clientIDs).Error; err != nil {
		return nil, err
	}
	if len(clientIDs) == 0 {
		return []models.Client{}, nil
	}
	var clients []models.Client
	if err := s.db.Where("id IN ?", clientIDs).Order("name ASC").Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// Update persists a client. The slug is re-normalized, and a slug that
// collides with a *different* client is refused.
func (s *ClientStore) Update(c *models.Client) error {
	c.Slug = models.NormalizeSlug(c.Slug)
	if c.Slug == "" {
		return errors.New("client slug is empty after normalization")
	}
	if other, _ := s.GetBySlug(c.Slug); other != nil && other.ID != c.ID {
		return fmt.Errorf("%w: %s", ErrSlugTaken, c.Slug)
	}
	return s.db.Save(c).Error
}

// Delete removes a client, its user-scope rows, and — the part that matters —
// leaves every device alive with `client_id = NULL`.
func (s *ClientStore) Delete(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Device{}).
			Where("client_id = ?", id).
			Update("client_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id = ?", id).Delete(&models.UserClient{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&models.Client{}).Error
	})
}

func (s *ClientStore) AssignDevices(clientID string, deviceIDs []string) (int64, error) {
	if len(deviceIDs) == 0 {
		return 0, nil
	}
	var target *string
	if trimmed := clientID; trimmed != "" {
		target = &trimmed
	}
	res := s.db.Model(&models.Device{}).Where("id IN ?", deviceIDs).Update("client_id", target)
	return res.RowsAffected, res.Error
}

func (s *ClientStore) DeviceIDsIn(clientIDs []string, includeUnassigned bool, deviceIDs []string) ([]string, error) {
	if len(deviceIDs) == 0 {
		return []string{}, nil
	}
	switch {
	case len(clientIDs) == 0 && !includeUnassigned:
		// A caller with no scope sees no devices. The empty set is an answer,
		// not a request to skip the filter.
		return []string{}, nil
	case includeUnassigned:
		if len(clientIDs) == 0 {
			return s.devicesWithQuery("client_id IS NULL", deviceIDs)
		}
		return s.devicesWithQuery("(client_id IS NULL OR client_id IN ?)", deviceIDs, clientIDs)
	default:
		return s.devicesWithQuery("client_id IN ?", deviceIDs, clientIDs)
	}
}

func (s *ClientStore) DeviceIDsNotIn(clientIDs []string, includeUnassigned bool, deviceIDs []string) ([]string, error) {
	in, err := s.DeviceIDsIn(clientIDs, includeUnassigned, deviceIDs)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(in))
	for _, id := range in {
		allowed[id] = true
	}
	out := make([]string, 0, len(deviceIDs)-len(in))
	for _, id := range deviceIDs {
		if !allowed[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *ClientStore) devicesWithQuery(clause string, deviceIDs []string, args ...any) ([]string, error) {
	var ids []string
	if err := s.db.Model(&models.Device{}).
		Where("id IN ?", deviceIDs).
		Where(clause, args...).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *ClientStore) ClientIDsForUser(userID string) ([]string, error) {
	var ids []string
	if err := s.db.Model(&models.UserClient{}).
		Where("user_id = ?", userID).
		Distinct().
		Pluck("client_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *ClientStore) AddUserClient(userID, clientID string) error {
	exists, err := s.GetByID(clientID)
	if err != nil {
		return err
	}
	if exists == nil {
		return errors.New("no such client")
	}
	var count int64
	if err := s.db.Model(&models.UserClient{}).
		Where("user_id = ? AND client_id = ?", userID, clientID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return s.db.Create(&models.UserClient{
		ID:       models.NewUserClientID(),
		UserID:   userID,
		ClientID: clientID,
	}).Error
}

// UsersForClient lists the accounts granted this client. The grants are the
// scope, so this is the ledger's "who can see this fleet" answer and the
// starting point of a `PUT /api/clients/:id/users` replace.
func (s *ClientStore) UsersForClient(clientID string) ([]models.User, error) {
	var rows []models.UserClient
	if err := s.db.Where("client_id = ?", clientID).Find(&rows).Error; err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		userIDs = append(userIDs, r.UserID)
	}
	if len(userIDs) == 0 {
		return []models.User{}, nil
	}
	var users []models.User
	if err := s.db.Where("id IN ?", userIDs).Order("username").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// GrantsCount answers "how many accounts is each client granted to" for every
// client at once, as one grouped read. The per-client ledger prints this
// number on every row, so a per-client call would make the ledger's cost scale
// with the tenant count - the N+1 the scale task (2.3-8) is written to catch.
func (s *ClientStore) GrantsCount() (map[string]int, error) {
	var rows []struct {
		ClientID string
		N        int
	}
	if err := s.db.Model(&models.UserClient{}).
		Select("client_id, count(*) AS n").Group("client_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.ClientID] = r.N
	}
	return out, nil
}

func (s *ClientStore) RemoveUserClient(userID, clientID string) error {
	return s.db.Where("user_id = ? AND client_id = ?", userID, clientID).
		Delete(&models.UserClient{}).Error
}
