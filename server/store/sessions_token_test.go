package store

import (
	"testing"

	"ourway/server/models"
)

func TestGetLiveByTokenHash(t *testing.T) {
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	s := &SessionStore{db: db}

	token, hash, err := models.NewRemoteToken()
	if err != nil {
		t.Fatal(err)
	}
	sess := &models.Session{
		ID: "11111111-1111-1111-1111-111111111111", DeviceID: "22222222-2222-2222-2222-222222222222",
		UserID: "33333333-3333-3333-3333-333333333333", Status: "pending", RemoteTokenHash: hash,
	}
	if err := s.Create(sess); err != nil {
		t.Fatal(err)
	}

	if got, err := s.GetLiveByTokenHash(models.HashRemoteToken(token)); err != nil || got.ID != sess.ID {
		t.Fatalf("live session not found by token: %v", err)
	}
	if _, err := s.GetLiveByTokenHash(models.HashRemoteToken("wrong")); err == nil {
		t.Fatal("wrong token must not match")
	}
	if err := s.EndSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLiveByTokenHash(models.HashRemoteToken(token)); err == nil {
		t.Fatal("ended session's token must not authenticate")
	}
}
