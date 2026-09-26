package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNpmSpec(t *testing.T) {
	tests := []struct {
		spec    string
		prefix  string
		version string
		match   bool
	}{
		{spec: "19.3.0", version: "19.3.0", match: true},
		{spec: "^2.13.2", prefix: "^", version: "2.13.2", match: true},
		{spec: "~1.2.3", prefix: "~", version: "1.2.3", match: true},
		{spec: "workspace:*"},
		{spec: "latest"},
		{spec: ">=1.4"},
		{spec: "1.0.0-beta.1"},
		{spec: "github:owner/repo"},
	}

	for _, test := range tests {
		t.Run(test.spec, func(t *testing.T) {
			m := npmSpec.FindStringSubmatch(test.spec)
			if (m != nil) != test.match {
				t.Fatalf("match = %v, want %v", m != nil, test.match)
			}

			if len(m) == 3 && (m[1] != test.prefix || m[2] != test.version) {
				t.Errorf("captured (%q, %q), want (%q, %q)", m[1], m[2], test.prefix, test.version)
			}
		})
	}
}

func TestPickVersion(t *testing.T) {
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)

	doc := func(latest string, times map[string]time.Time) packument {
		times["created"] = old
		times["modified"] = now

		return packument{DistTags: map[string]string{"latest": latest}, Time: times}
	}

	tests := []struct {
		name string
		doc  packument
		have string
		held bool
		want string
	}{
		{
			name: "newest old enough release",
			doc:  doc("2.1.0", map[string]time.Time{"1.0.0": old, "2.0.0": old, "2.1.0": old}),
			have: "1.0.0",
			want: "2.1.0",
		},
		{
			// A release that could still be pulled for being hijacked waits.
			name: "a fresh release waits out the cooldown",
			doc:  doc("2.1.0", map[string]time.Time{"1.0.0": old, "2.0.0": old, "2.1.0": fresh}),
			have: "1.0.0",
			want: "2.0.0",
		},
		{
			name: "prereleases and versions above the latest tag are ignored",
			doc: doc("2.0.0", map[string]time.Time{
				"1.0.0": old, "2.0.0": old, "3.0.0-rc.1": old, "3.0.0": old,
			}),
			have: "1.0.0",
			want: "2.0.0",
		},
		{
			name: "a held package stays on its major",
			doc:  doc("7.0.2", map[string]time.Time{"5.9.3": old, "5.9.4": old, "7.0.2": old}),
			have: "5.9.3",
			held: true,
			want: "5.9.4",
		},
		{
			// A pin ahead of the latest tag is somebody's deliberate choice.
			name: "never backwards",
			doc:  doc("1.0.0", map[string]time.Time{"1.0.0": old, "1.1.0": old}),
			have: "1.1.0",
			want: "1.1.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := pickVersion(test.doc, test.have, test.held, now)
			if err != nil {
				t.Fatalf("pickVersion: %v", err)
			}

			if got != test.want {
				t.Errorf("pickVersion = %q, want %q", got, test.want)
			}
		})
	}

	_, err := pickVersion(packument{DistTags: map[string]string{}}, "1.0.0", false, now)
	if !errors.Is(err, errNoCandidate) {
		t.Errorf("pickVersion without a latest tag: error = %v, want %v", err, errNoCandidate)
	}
}

func TestSetSpec(t *testing.T) {
	const manifest = `{
  "dependencies": {
    "@cloudflare/kumo": "^2.13.2",
    "react": "19.3.0"
  }
}
`

	got, err := setSpec(manifest, dep{Name: "@cloudflare/kumo", Prefix: "^", Version: "2.13.2"}, "2.14.0")
	if err != nil {
		t.Fatalf("setSpec: %v", err)
	}

	want := `{
  "dependencies": {
    "@cloudflare/kumo": "^2.14.0",
    "react": "19.3.0"
  }
}
`
	if got != want {
		t.Errorf("setSpec =\n%s\nwant\n%s", got, want)
	}

	const twice = `{"dependencies": {"react": "19.3.0"}, "devDependencies": {"react": "19.3.0"}}`

	_, err = setSpec(twice, dep{Name: "react", Version: "19.3.0"}, "19.4.0")
	if !errors.Is(err, errSpecNotUnique) {
		t.Errorf("setSpec on a duplicated package: error = %v, want %v", err, errSpecNotUnique)
	}
}

func TestFamilyOf(t *testing.T) {
	tests := map[string]string{
		"react":                      "react",
		"react-dom":                  "react",
		"@types/react":               "react",
		"@types/react-dom":           "react",
		"@types/node":                "node",
		"@tanstack/react-query":      "@tanstack",
		"@tanstack/router-plugin":    "@tanstack",
		"vitest":                     "@vitest",
		"@vitest/browser-playwright": "@vitest",
		"oxlint-tsgolint":            "@oxlint",
		"openapi-fetch":              "openapi-ts",
		"valibot":                    "valibot",
	}

	for name, want := range tests {
		if got := familyOf(name); got != want {
			t.Errorf("familyOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// Every hold has to name a package the manifests still pin, or it silently
// stops holding anything.
func TestRepoPackageHolds(t *testing.T) {
	r := &repo{Root: "../.."}

	for _, set := range packageSets {
		deps, err := readDeps(r, set)
		if err != nil {
			t.Fatalf("%s: %v", set.Name, err)
		}

		pinned := map[string]bool{}
		for _, d := range deps {
			pinned[d.File+":"+d.Name] = true
		}

		for key := range set.Holds {
			if !pinned[key] {
				t.Errorf("%s: hold %q names no pinned package", set.Name, key)
			}
		}
	}
}

// A scoped name has to reach the registry with its slash escaped, or the
// registry answers for the scope instead of the package.
func TestResolveDepAgainstRegistry(t *testing.T) {
	old := time.Now().Add(-30 * 24 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/@tanstack%2Freact-query" {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(packument{
			DistTags: map[string]string{"latest": "5.103.0"},
			Time:     map[string]time.Time{"5.102.8": old, "5.103.0": old},
		})
	}))
	t.Cleanup(srv.Close)

	previous := npmRegistry
	npmRegistry = srv.URL

	t.Cleanup(func() { npmRegistry = previous })

	got, err := resolveDep(t.Context(), packageSets[0],
		dep{File: "web/package.json", Name: "@tanstack/react-query", Version: "5.102.8"})
	if err != nil {
		t.Fatalf("resolveDep: %v", err)
	}

	if got != "5.103.0" {
		t.Errorf("resolveDep = %q, want 5.103.0", got)
	}
}
