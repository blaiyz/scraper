package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestStartScraper_Valid(t *testing.T) {
	// Create a test server with endpoints for valid and dead links.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			// The root page has two links: one valid (/page) and one dead (/dead).
			fmt.Fprintf(w, `<html><body><a href="/page">page</a><a href="/dead">dead</a></body></html>`)
		case "/page":
			// A valid page with no further links.
			fmt.Fprintf(w, `<html><body>No further links</body></html>`)
		case "/dead":
			// A dead link (returns 404).
			http.Error(w, "Not Found", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Run StartScraper on our test server.
	// Using a couple of workers.
	deadLinks, err := StartScraper(ts.URL, 10)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// We expect the dead link to be reported.
	expectedDead := ts.URL + "/dead"
	found := slices.Contains(deadLinks, expectedDead)

	if !found {
		t.Errorf("Expected dead link %q not found in: %v", expectedDead, deadLinks)
	}
}

func TestStartScraper_InvalidURL(t *testing.T) {
	// Passing an invalid URL should return an error.
	_, err := StartScraper("invalid-url", 1)
	if err == nil {
		t.Errorf("Expected error for invalid URL, got nil")
	}
}
