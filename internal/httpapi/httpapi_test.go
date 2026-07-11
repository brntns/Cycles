// API tests for the cycle state machine over real HTTP against a real
// Postgres, per SPEC: early termination must be possible anytime — burying
// from every non-terminal state, forward transitions at any pace (a full
// same-day walk to completed), and the guard rails on completed.
//
// They need a database: set TEST_DATABASE_URL (e.g. a throwaway
// postgres:16-alpine container); without it the tests skip.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"varde/internal/auth"
	"varde/internal/dbmig"
	"varde/internal/model"
)

const testPassword = "test-password"

type testClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping API tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := dbmig.Run(ctx, pool); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	// Each test starts from an empty world (single-active-cycle rule!).
	if _, err := pool.Exec(ctx, `
		TRUNCATE timeline_entries, weekly_reviews, quarterly_reviews,
		         parked_questions, ideas, cycles, sessions CASCADE
	`); err != nil {
		t.Fatalf("truncate test db: %v", err)
	}

	authSvc := auth.NewService(pool, testPassword, false, 90)
	srv := NewServer(pool, authSvc, fstest.MapFS{})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	c := &testClient{t: t, base: ts.URL, http: &http.Client{Jar: jar}}

	resp := c.do(http.MethodPost, "/auth/login", map[string]any{"password": testPassword}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	return c
}

// do sends a JSON request; if out is non-nil the response body is decoded
// into it. The caller checks resp.StatusCode.
func (c *testClient) do(method, path string, body any, out any) *http.Response {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, c.base+path, &buf)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatalf("decode %s %s response: %v", method, path, err)
		}
	}
	return resp
}

func (c *testClient) createCycle(title string) *model.Cycle {
	c.t.Helper()
	var cy model.Cycle
	resp := c.do(http.MethodPost, "/cycles", map[string]any{
		"title": title, "intent": "test intent", "show_plan": "a repo",
	}, &cy)
	if resp.StatusCode != http.StatusCreated {
		c.t.Fatalf("create cycle: status %d", resp.StatusCode)
	}
	return &cy
}

// walkTo advances a fresh building cycle one legal step at a time until it
// reaches target (understanding or showing).
func (c *testClient) walkTo(cy *model.Cycle, target model.CycleState) *model.Cycle {
	c.t.Helper()
	order := []model.CycleState{model.StateUnderstanding, model.StateShowing}
	for _, next := range order {
		if cy.State == target {
			break
		}
		var updated model.Cycle
		resp := c.do(http.MethodPatch, "/cycles/"+cy.ID, map[string]any{"state": next}, &updated)
		if resp.StatusCode != http.StatusOK {
			c.t.Fatalf("walk to %s: status %d", next, resp.StatusCode)
		}
		cy = &updated
	}
	if cy.State != target {
		c.t.Fatalf("walkTo: wanted %s, got %s", target, cy.State)
	}
	return cy
}

func (c *testClient) patchStatus(id string, body map[string]any) (int, map[string]any) {
	c.t.Helper()
	var out map[string]any
	resp := c.do(http.MethodPatch, "/cycles/"+id, body, &out)
	return resp.StatusCode, out
}

// newestSystemEntry returns the newest timeline entry, which must be a
// system entry (the terminal event on an ended cycle).
func (c *testClient) newestEntry(cycleID string) *model.TimelineEntry {
	c.t.Helper()
	var entries []*model.TimelineEntry
	resp := c.do(http.MethodGet, "/cycles/"+cycleID+"/entries", nil, &entries)
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("list entries: status %d", resp.StatusCode)
	}
	if len(entries) == 0 {
		c.t.Fatal("timeline is empty")
	}
	return entries[0]
}

// Burying must work at any time from every non-terminal state, requiring
// only the brain-dump — never gated behind a review or a minimum age.
func TestBuryFromEveryNonTerminalState(t *testing.T) {
	states := []model.CycleState{model.StateBuilding, model.StateUnderstanding, model.StateShowing}
	for _, from := range states {
		t.Run(string(from), func(t *testing.T) {
			c := newTestClient(t)
			cy := c.walkTo(c.createCycle("bury from "+string(from)), from)

			// Without a brain-dump the burial is rejected.
			code, errBody := c.patchStatus(cy.ID, map[string]any{"state": "buried"})
			if code != http.StatusUnprocessableEntity {
				t.Fatalf("bury without brain_dump: want 422, got %d", code)
			}
			if errBody["error"] != "missing_brain_dump" {
				t.Fatalf("bury without brain_dump: want missing_brain_dump, got %v", errBody["error"])
			}

			// With one it succeeds — same day the cycle was created.
			var buried model.Cycle
			resp := c.do(http.MethodPatch, "/cycles/"+cy.ID, map[string]any{
				"state": "buried", "brain_dump": "learned plenty; stopping on purpose",
			}, &buried)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("bury from %s: want 200, got %d", from, resp.StatusCode)
			}
			if buried.State != model.StateBuried {
				t.Fatalf("state: want buried, got %s", buried.State)
			}
			if buried.EndedAt == nil {
				t.Fatal("ended_at not set on burial")
			}

			// The timeline closes with a terminal system entry.
			newest := c.newestEntry(cy.ID)
			if newest.Kind != model.EntrySystem || newest.Text != "Cycle buried" {
				t.Fatalf("terminal entry: want system %q, got %s %q", "Cycle buried", newest.Kind, newest.Text)
			}
		})
	}
}

// Forward transitions are never time-gated: the full walk
// building → understanding → showing → completed may happen on one day.
func TestSameDayWalkToCompleted(t *testing.T) {
	c := newTestClient(t)
	cy := c.walkTo(c.createCycle("one-day cycle"), model.StateShowing)

	var done model.Cycle
	resp := c.do(http.MethodPatch, "/cycles/"+cy.ID, map[string]any{
		"state":        "completed",
		"artifact_url": "https://example.com/shown",
		"brain_dump":   "shipped in a day; here is what I learned",
	}, &done)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete: want 200, got %d", resp.StatusCode)
	}
	if done.State != model.StateCompleted {
		t.Fatalf("state: want completed, got %s", done.State)
	}
	if done.EndedAt == nil {
		t.Fatal("ended_at not set on completion")
	}

	newest := c.newestEntry(cy.ID)
	if newest.Kind != model.EntrySystem || newest.Text != "Cycle completed" {
		t.Fatalf("terminal entry: want system %q, got %s %q", "Cycle completed", newest.Kind, newest.Text)
	}

	// The single-active-cycle slot is free again immediately.
	next := c.createCycle("the next cycle")
	if next.State != model.StateBuilding {
		t.Fatalf("next cycle state: want building, got %s", next.State)
	}
}

// completed stays guarded: no artifact or no brain-dump means a clear 422,
// and the state machine still refuses to skip states.
func TestCompletedRejections(t *testing.T) {
	c := newTestClient(t)
	cy := c.walkTo(c.createCycle("guarded cycle"), model.StateShowing)

	code, body := c.patchStatus(cy.ID, map[string]any{"state": "completed"})
	if code != http.StatusUnprocessableEntity || body["error"] != "missing_artifact" {
		t.Fatalf("no artifact: want 422 missing_artifact, got %d %v", code, body["error"])
	}

	code, body = c.patchStatus(cy.ID, map[string]any{
		"state": "completed", "artifact_url": "https://example.com/shown",
	})
	if code != http.StatusUnprocessableEntity || body["error"] != "missing_brain_dump" {
		t.Fatalf("no brain_dump: want 422 missing_brain_dump, got %d %v", code, body["error"])
	}

	code, body = c.patchStatus(cy.ID, map[string]any{
		"state": "completed", "brain_dump": "words",
	})
	if code != http.StatusUnprocessableEntity || body["error"] != "missing_artifact" {
		t.Fatalf("brain_dump only: want 422 missing_artifact, got %d %v", code, body["error"])
	}

	// Still in showing after all the rejections — nothing half-applied.
	var current model.Cycle
	resp := c.do(http.MethodGet, "/cycles/"+cy.ID, nil, &current)
	if resp.StatusCode != http.StatusOK || current.State != model.StateShowing {
		t.Fatalf("after rejections: want showing, got %d %s", resp.StatusCode, current.State)
	}
}

func TestCompletedCannotSkipStates(t *testing.T) {
	c := newTestClient(t)
	cy := c.createCycle("no shortcuts")

	code, body := c.patchStatus(cy.ID, map[string]any{
		"state":        "completed",
		"artifact_url": "https://example.com/shown",
		"brain_dump":   "words",
	})
	if code != http.StatusUnprocessableEntity || body["error"] != "invalid_transition" {
		t.Fatalf("building → completed: want 422 invalid_transition, got %d %v", code, body["error"])
	}
}
