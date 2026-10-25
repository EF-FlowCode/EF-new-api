package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCSVSelection(t *testing.T) {
	got, err := csvSelection(strings.NewReader("\ufefflog_id,note\r\n12,\"a,b\"\r\n12,duplicate\r\n13,ok\r\n"), "log_id", "log")
	if err != nil {
		t.Fatal(err)
	}
	ids := got["log_ids"].([]int64)
	if len(ids) != 2 || ids[0] != 12 || ids[1] != 13 {
		t.Fatalf("%v", got)
	}
	for _, input := range []string{"log_id\n", "wrong\n12\n", "log_id\n-1\n", "log_id\n12.5\n"} {
		if _, err := csvSelection(strings.NewReader(input), "log_id", "log"); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestPreviewExecuteRequiresMatchingConfirmation(t *testing.T) {
	dir := t.TempDir()
	csvPath, previewPath := filepath.Join(dir, "input.csv"), filepath.Join(dir, "preview.json")
	if err := os.WriteFile(csvPath, []byte("log_id,quota\n12,99999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	executions := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "/preview") {
			if body["quota"] != nil || body["log_ids"] == nil {
				t.Errorf("CSV must supply identifiers only: %v", body)
			}
		} else if strings.HasSuffix(r.URL.Path, "/execute") {
			executions++
			if body["expected_quota"] != float64(30) || body["expected_count"] != float64(1) {
				t.Errorf("confirmation: %v", body)
			}
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"batch-test","quota":30,"count":1,"status":"preview"}}`))
	}))
	defer server.Close()
	t.Setenv("NEW_API_URL", server.URL)
	t.Setenv("NEW_API_ADMIN_ID", "99")
	t.Setenv("NEW_API_ADMIN_TOKEN", "test-only")
	if err := run([]string{"preview", "--csv", csvPath, "--reason", "test", "--file", previewPath}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"execute", "--file", previewPath}); err == nil {
		t.Fatal("execution accepted without approval")
	}
	if err := run([]string{"execute", "--file", previewPath, "--confirm", "wrong"}); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if executions != 0 {
		t.Fatal("unapproved execution reached server")
	}
	if err := run([]string{"execute", "--file", previewPath, "--confirm", "batch-test"}); err != nil {
		t.Fatal(err)
	}
	if executions != 1 {
		t.Fatalf("executions=%d", executions)
	}
}

func TestAPIRequestFailureAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("New-Api-User") != "99" || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing admin authentication")
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"message":"stale preview"}`))
	}))
	defer server.Close()
	_, err := apiRequest(server.Client(), server.URL, "99", "test", http.MethodPost, "/test", nil)
	if err == nil || !strings.Contains(err.Error(), "stale preview") {
		t.Fatalf("%v", err)
	}
}

func TestScopedTokenNeedsNoAdminID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("New-Api-User") != "" || r.Header.Get("Authorization") != "Bearer nap_scoped" {
			t.Errorf("unexpected headers %q %q", r.Header.Get("New-Api-User"), r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer server.Close()
	if _, err := apiRequest(server.Client(), server.URL, "", "nap_scoped", http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NEW_API_ADMIN_ID", "")
	t.Setenv("NEW_API_ADMIN_TOKEN", "legacy-token")
	if err := run([]string{"list", "-server", server.URL}); err == nil || !strings.Contains(err.Error(), "NEW_API_ADMIN_ID") {
		t.Fatalf("legacy tokens still need the admin id: %v", err)
	}
}
