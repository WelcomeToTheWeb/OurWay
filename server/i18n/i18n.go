// Package i18n holds the strings the **server** renders — outbound mail, in
// practice — in the three locales the product ships.
//
// The console's bundles live in `web/src/i18n/locales`. These live here because
// they are compiled into the server: a notification that reaches a person is as
// localizable as the page that shows them the same fact, and a mail body that
// only exists in English is the same defect the 2.2 locale gate was written to
// catch, one mailbox away. `scripts/i18n-parity.py` checks both bundles.
package i18n

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/en.json locales/de.json locales/fr.json
var bundles embed.FS

// Locales is every locale the server can render, in the order the console
// offers them.
var Locales = []string{"en", "de", "fr"}

// FallbackLocale is what a missing string falls back to. A mail body that
// renders an empty line is worse than one in a language the recipient may not
// prefer: the body still states the unit, the metric and the number.
const FallbackLocale = "en"

var (
	loadOnce sync.Once
	trees    map[string]map[string]any
	loadErr  error
)

func load() {
	loadOnce.Do(func() {
		trees = map[string]map[string]any{}
		for _, code := range Locales {
			raw, err := bundles.ReadFile("locales/" + code + ".json")
			if err != nil {
				loadErr = err
				return
			}
			var tree map[string]any
			if err := json.Unmarshal(raw, &tree); err != nil {
				loadErr = err
				return
			}
			trees[code] = tree
		}
	})
}

// LoadError reports why the embedded bundles cannot be read. A non-nil value
// means every lookup falls back to the caller's default, which is a state an
// operator should be able to see, not one that fails silently at the first send.
func LoadError() error {
	load()
	return loadErr
}

// String returns the localized string at `path` (dot-separated), falling back to
// the fallback locale when the locale does not carry it. An empty string means
// no locale carries the path.
func String(locale, path string) string {
	load()
	if trees == nil {
		return ""
	}
	if value := lookup(trees[locale], path); value != "" {
		return value
	}
	return lookup(trees[FallbackLocale], path)
}

func lookup(tree map[string]any, path string) string {
	if tree == nil {
		return ""
	}
	current := any(tree)
	for _, part := range strings.Split(path, ".") {
		node, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = node[part]
		if !ok {
			return ""
		}
	}
	value, _ := current.(string)
	return value
}

// Placeholders returns the `{{.X}}` placeholders a template string uses, sorted.
// The test suite compares them across locales: a German body that references a
// field the renderer never supplies renders a blank line to a German operator,
// and the parity script's key comparison cannot see that.
func Placeholders(template string) []string {
	var found []string
	for _, part := range strings.Split(template, "{{") {
		if strings.HasPrefix(part, ".") {
			name := strings.SplitN(part, "}", 2)[0]
			name = strings.TrimSpace(strings.TrimPrefix(name, "."))
			if name != "" {
				found = append(found, name)
			}
		}
	}
	sort.Strings(found)
	return found
}
