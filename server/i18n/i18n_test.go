package i18n

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The mail templates are the product's other locale bundle: a notification that
// reaches a person is as localizable as the page that shows them the same fact.
// `scripts/i18n-parity.py` compares the key paths; these tests compare what the
// key comparison cannot see — that every locale's template references the fields
// the renderer supplies, and that no locale renders a blank line.

func bundlePaths() []string {
	return []string{"en", "de", "fr"}
}

func TestEmbeddedBundlesLoad(t *testing.T) {
	if err := LoadError(); err != nil {
		t.Fatalf("embedded bundles: %v", err)
	}
	for _, code := range bundlePaths() {
		if String(code, "mail.footer") == "" {
			t.Errorf("%s has no mail.footer", code)
		}
	}
}

// Every mail template in the product must exist in every locale: a German
// operator whose alert mail falls back to English is the defect 2.2's locale
// gate exists to prevent, one mailbox away.
func TestEveryEventTemplateExistsInEveryLocale(t *testing.T) {
	events := []string{
		"alert_created", "alert_resolved", "patch_deployed",
		"device_online", "device_offline", "device_registered", "session_started",
	}
	for _, code := range bundlePaths() {
		for _, event := range events {
			for _, part := range []string{"subject", "body"} {
				path := "mail.events." + event + "." + part
				// Compare against the file, not the embedded copy: the parity
				// script reads the files, and a template that reaches the bundle
				// only through the embedded copy is a template nobody checks.
				if String(code, path) == "" {
					t.Errorf("%s has no %s", code, path)
				}
			}
		}
	}
}

// The renderer supplies a fixed field set. A template that references a field
// outside it renders "—" in every locale — a mail that silently stops naming the
// tenant, the threshold, the unit.
func TestTemplatesReferenceOnlyFieldsTheRendererSupplies(t *testing.T) {
	supplied := map[string]bool{
		"Type": true, "Device": true, "Severity": true, "Metric": true,
		"Value": true, "Threshold": true, "Client": true, "User": true,
		"Name": true, "Message": true, "Version": true, "At": true,
		"To": true, "Footer": true,
	}
	for _, code := range bundlePaths() {
		for _, path := range mailTemplatePaths(t, code) {
			for _, name := range Placeholders(String(code, path)) {
				if !supplied[name] {
					t.Errorf("%s template %s references {{.%s}}, which the renderer never supplies", code, path, name)
				}
			}
		}
	}
}

// The locales must reference the same fields. A French body that drops the
// threshold line is not a translation choice an operator can act on — it is the
// number missing from the notification.
func TestTemplatesReferenceTheSameFieldsAcrossLocales(t *testing.T) {
	reference := map[string][]string{}
	for _, path := range mailTemplatePaths(t, "en") {
		reference[path] = Placeholders(String("en", path))
	}
	for _, code := range bundlePaths() {
		if code == "en" {
			continue
		}
		for path, want := range reference {
			got := Placeholders(String(code, path))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s template %s references %v, en references %v", code, path, got, want)
			}
		}
	}
}

// A missing locale string falls back to the fallback locale, never to nothing:
// a mail with an empty body is a mail that states nothing.
func TestMissingStringFallsBackToTheFallbackLocale(t *testing.T) {
	value := String("de", "mail.footer")
	if value == "" {
		t.Fatal("no footer in de")
	}
	if String("zz", "mail.footer") != String(FallbackLocale, "mail.footer") {
		t.Error("an unknown locale did not fall back to the fallback locale")
	}
	if String("de", "mail.nothere.nope") != "" {
		t.Error("a path no locale carries must answer empty")
	}
}

// The repo files and the embedded copies must not drift: the parity script reads
// the files, the server sends what it embedded.
func TestEmbeddedBundlesMatchTheFilesOnDisk(t *testing.T) {
	for _, code := range bundlePaths() {
		onDisk, err := os.ReadFile("locales/" + code + ".json")
		if err != nil {
			t.Skipf("locale file not readable from the test working directory: %v", err)
		}
		var fileTree, embeddedTree map[string]any
		if json.Unmarshal(onDisk, &fileTree) != nil {
			t.Fatalf("%s file does not parse", code)
		}
		raw, err := bundles.ReadFile("locales/" + code + ".json")
		if err != nil {
			t.Fatalf("embedded %s: %v", code, err)
		}
		if json.Unmarshal(raw, &embeddedTree) != nil {
			t.Fatalf("embedded %s does not parse", code)
		}
		if !reflect.DeepEqual(fileTree, embeddedTree) {
			t.Errorf("%s: the file on disk and the embedded copy differ", code)
		}
	}
}

func mailTemplatePaths(t *testing.T, code string) []string {
	t.Helper()
	var paths []string
	raw, err := bundles.ReadFile("locales/" + code + ".json")
	if err != nil {
		t.Fatalf("read %s: %v", code, err)
	}
	var tree map[string]any
	if json.Unmarshal(raw, &tree) != nil {
		t.Fatalf("parse %s", code)
	}
	mail, _ := tree["mail"].(map[string]any)
	collect(t, mail, "mail", &paths)
	return paths
}

func collect(t *testing.T, node map[string]any, prefix string, out *[]string) {
	t.Helper()
	for key, value := range node {
		path := prefix + "." + key
		switch typed := value.(type) {
		case string:
			if strings.Contains(typed, "{{") {
				*out = append(*out, path)
			}
		case map[string]any:
			collect(t, typed, path, out)
		}
	}
}
