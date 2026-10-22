package remote

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"keybar/vault"
)

const dummyToken = "dummy-api-token-0000"

func tokens(name string) ([]byte, error) {
	if name == GitHubTokenKey || name == VercelTokenKey || name == "MY_VT" {
		return []byte(dummyToken), nil
	}
	return nil, vault.ErrNotFound
}

func TestGitHub(t *testing.T) {
	pub, priv, _ := box.GenerateKey(rand.Reader)
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+dummyToken {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/o/r/actions/secrets/public-key":
			_ = json.NewEncoder(w).Encode(map[string]string{"key_id": "kid1", "key": base64.StdEncoding.EncodeToString(pub[:])})
		case r.Method == "PUT" && r.URL.Path == "/repos/o/r/actions/secrets/DEPLOY_TOKEN":
			var body struct{ EncryptedValue, KeyID string }
			_ = json.NewDecoder(r.Body).Decode(&struct {
				E *string `json:"encrypted_value"`
				K *string `json:"key_id"`
			}{&body.EncryptedValue, &body.KeyID})
			sealed, _ := base64.StdEncoding.DecodeString(body.EncryptedValue)
			plain, ok := box.OpenAnonymous(nil, sealed, pub, priv)
			if !ok || body.KeyID != "kid1" {
				w.WriteHeader(422)
				return
			}
			got = plain
			w.WriteHeader(201)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	s := &Syncer{Token: tokens, GitHubURL: srv.URL}
	p := vault.Platform{Kind: "github", Target: "o/r", Var: "DEPLOY_TOKEN"}
	if err := s.Update(context.Background(), "RAILWAY_TOKEN", p, []byte("dummy-new-value")); err != nil {
		t.Fatal(err)
	}
	if string(got) != "dummy-new-value" {
		t.Errorf("server got %q", got)
	}
	p.Target = "o/missing"
	if err := s.Update(context.Background(), "X", p, []byte("v")); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing repo: %v", err)
	}
}

func TestGitHubFallsBackToGH(t *testing.T) {
	var args []string
	var stdin []byte
	s := &Syncer{Token: func(string) ([]byte, error) { return nil, vault.ErrNotFound },
		GH: func(_ context.Context, a []string, in []byte) error { args, stdin = a, in; return nil }}
	if err := s.Update(context.Background(), "K", vault.Platform{Kind: "github", Target: "o/r"}, []byte("dummy-v")); err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "secret set K --repo o/r" || string(stdin) != "dummy-v" {
		t.Errorf("%v %q", args, stdin)
	}
	for _, a := range args {
		if strings.Contains(a, "dummy-v") {
			t.Error("value in argv")
		}
	}
}

func TestVercel(t *testing.T) {
	var body map[string]any
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v10/projects/my-app/env" || r.Header.Get("Authorization") != "Bearer "+dummyToken {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"bad"}}`))
			return
		}
		query = r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(201)
	}))
	defer srv.Close()
	s := &Syncer{Token: tokens, VercelURL: srv.URL}
	p := vault.Platform{Kind: "vercel", Target: "my-app", Envs: []string{"production"}, Team: "team_1", TokenKey: "MY_VT"}
	if err := s.Update(context.Background(), "DB_URL", p, []byte("dummy-db")); err != nil {
		t.Fatal(err)
	}
	if body["key"] != "DB_URL" || body["value"] != "dummy-db" || body["type"] != "encrypted" || query != "teamId=team_1&upsert=true" {
		t.Errorf("%v %s", body, query)
	}
	p.Target = "other"
	if err := s.Update(context.Background(), "DB_URL", p, []byte("v")); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Errorf("error: %v", err)
	}
	s.Token = func(string) ([]byte, error) { return nil, vault.ErrNotFound }
	if err := s.Update(context.Background(), "DB_URL", vault.Platform{Kind: "vercel", Target: "x"}, []byte("v")); err == nil || !strings.Contains(err.Error(), "VERCEL_TOKEN") {
		t.Errorf("no token: %v", err)
	}
}

func TestManual(t *testing.T) {
	s := &Syncer{}
	if err := s.Update(context.Background(), "K", vault.Platform{Kind: "railway"}, nil); err == nil {
		t.Error("railway updated")
	}
	_ = errors.New
}
