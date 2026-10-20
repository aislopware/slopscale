package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParseUnstable(t *testing.T) {
	tests := []struct {
		version string
		base    string
		patch   int
		ok      bool
	}{
		{version: "1.105.59", base: "v1.105.0-pre", patch: 59, ok: true},
		{version: "1.107.1", base: "v1.107.0-pre", patch: 1, ok: true},
		{version: "1.104.1"},  // stable track
		{version: "1.105.0"},  // the opening tag itself is not a build
		{version: "1.105"},    // not major.minor.patch
		{version: "1.105.x"},  // not a number
		{version: "1.105.-3"}, // negative
		{version: ""},
	}

	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			base, patch, err := parseUnstable(test.version)
			if !test.ok {
				if !errors.Is(err, errNotUnstable) {
					t.Fatalf("parseUnstable(%q) error = %v, want %v", test.version, err, errNotUnstable)
				}

				return
			}

			if err != nil || base != test.base || patch != test.patch {
				t.Errorf("parseUnstable(%q) = %q, %d, %v; want %q, %d", test.version, base, patch, err,
					test.base, test.patch)
			}
		})
	}
}

// fakeGitHub serves main as a straight line of commits c1, c2, ... after the
// cycle's opening tag, paging the walk the way GitHub's compare endpoint does.
// skew shifts what the check reports, to model a numbering scheme that no
// longer matches.
func fakeGitHub(t *testing.T, commits, skew int) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/tailscale/tailscale/compare/v1.105.0-pre...main":
			perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))

			var list []string
			for i := (page-1)*perPage + 1; i <= min(page*perPage, commits); i++ {
				list = append(list, fmt.Sprintf(`{"sha":"c%d"}`, i))
			}

			fmt.Fprintf(w, `{"commits":[%s]}`, strings.Join(list, ","))
		default:
			var n int

			_, err := fmt.Sscanf(r.URL.Path, "/repos/tailscale/tailscale/compare/v1.105.0-pre...c%d", &n)
			if err != nil {
				http.NotFound(w, r)

				return
			}

			fmt.Fprintf(w, `{"ahead_by":%d,"behind_by":0}`, n+skew)
		}
	}))
	t.Cleanup(srv.Close)

	previous := githubAPI
	githubAPI = srv.URL

	t.Cleanup(func() { githubAPI = previous })
}

func TestUnstableCommit(t *testing.T) {
	t.Run("first page", func(t *testing.T) {
		fakeGitHub(t, 300, 0)

		got, err := unstableCommit(t.Context(), "1.105.59")
		if err != nil || got != "c59" {
			t.Fatalf("unstableCommit = %q, %v; want c59", got, err)
		}
	})

	t.Run("a later page", func(t *testing.T) {
		fakeGitHub(t, 300, 0)

		got, err := unstableCommit(t.Context(), "1.105.201")
		if err != nil || got != "c201" {
			t.Fatalf("unstableCommit = %q, %v; want c201", got, err)
		}
	})

	t.Run("a release past the end of main", func(t *testing.T) {
		fakeGitHub(t, 40, 0)

		_, err := unstableCommit(t.Context(), "1.105.59")
		if !errors.Is(err, errUnstableCommit) {
			t.Fatalf("error = %v, want %v", err, errUnstableCommit)
		}
	})

	t.Run("a count that no longer matches the version", func(t *testing.T) {
		fakeGitHub(t, 300, 1)

		_, err := unstableCommit(t.Context(), "1.105.59")
		if !errors.Is(err, errUnstableCommit) {
			t.Fatalf("error = %v, want %v", err, errUnstableCommit)
		}
	})
}

func TestLatestUnstable(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		err              bool
	}{
		{name: "containers version", body: `{"Version":"1.105.59","ExesVersion":"1.105.78"}`, want: "1.105.59"},
		{name: "no version", body: `{"Tarballs":{}}`, err: true},
		{name: "not json", body: `<html>`, err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, test.body)
			}))
			t.Cleanup(srv.Close)

			previous := unstableURL
			unstableURL = srv.URL

			t.Cleanup(func() { unstableURL = previous })

			got, err := latestUnstable(t.Context())
			if (err != nil) != test.err || got != test.want {
				t.Errorf("latestUnstable = %q, %v; want %q (error %v)", got, err, test.want, test.err)
			}
		})
	}
}
