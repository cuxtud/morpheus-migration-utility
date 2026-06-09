package morpheus

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginWithPassword_oauthRequestShape(t *testing.T) {
	var gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"bearer"}`))
	}))
	defer srv.Close()

	token, err := LoginWithPassword(srv.URL, "anish", "test01", true)
	if err != nil {
		t.Fatal(err)
	}
	if token != "test-token" {
		t.Fatalf("token %q", token)
	}
	if !strings.Contains(gotQuery, "client_id=morph-api") {
		t.Fatalf("query missing client_id: %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "grant_type=password") {
		t.Fatalf("query missing grant_type: %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "scope=write") {
		t.Fatalf("query missing scope: %s", gotQuery)
	}
	if strings.Contains(gotQuery, "username=") {
		t.Fatalf("username must not be in query: %s", gotQuery)
	}
	if gotBody != "password=test01&username=anish" && gotBody != "username=anish&password=test01" {
		t.Fatalf("body %q", gotBody)
	}
}
