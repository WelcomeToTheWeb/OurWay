package models

import (
	"reflect"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{" Prod ", "prod", "web:eu-1", "bad tag", "", "A_b.c", "waytoolongwaytoolongwaytoolongwaytoolong"})
	want := []string{"prod", "web:eu-1", "a_b.c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if NormalizeTags(nil) == nil {
		t.Fatal("must never be nil")
	}
}

func TestHasAnyTag(t *testing.T) {
	d := &Device{Tags: []string{"prod", "web"}}
	if !d.HasAnyTag([]string{"db", "web"}) || d.HasAnyTag([]string{"db"}) {
		t.Fatal("HasAnyTag wrong")
	}
}
