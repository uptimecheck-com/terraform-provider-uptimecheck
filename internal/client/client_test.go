package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorsNotFoundAndRateLimits(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer uc_test" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/monitors/missing":
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"code":"not_found","message":"Monitor not found."}}`))
		case "/monitors":
			w.WriteHeader(422)
			w.Write([]byte(`{"error":{"code":"validation_failed","message":"Check the highlighted fields.","fields":{"intervalSeconds":"Pick an interval."}}}`))
		case "/busy":
			calls++
			if calls == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				return
			}
			w.Write([]byte(`{"data":[{"id":"prj_1","name":"Acme","color":"#1F5CFF"}]}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "uc_test", "test")
	ctx := context.Background()

	err := c.Do(ctx, "GET", "/monitors/missing", nil, nil)
	if !NotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
	err = c.Do(ctx, "POST", "/monitors", map[string]string{"name": "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "intervalSeconds: Pick an interval.") || NotFound(err) {
		t.Fatalf("validation error not explained: %v", err)
	}
	var projects []Project
	if err := c.Do(ctx, "GET", "/busy", nil, &projects); err != nil || len(projects) != 1 || projects[0].Name != "Acme" || calls != 2 {
		t.Fatalf("rate limit not retried: %v %v calls=%d", err, projects, calls)
	}
}
