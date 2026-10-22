// Package remote updates a key's new value where else it is stored, through
// their APIs: GitHub Actions secrets and Vercel environment variables.
package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"

	"keybar/vault"
)

// Default token keys: the KeyBar keys holding the API tokens.
const (
	GitHubTokenKey = "GITHUB_TOKEN"
	VercelTokenKey = "VERCEL_TOKEN"
)

// Syncer updates platforms. Token reads a KeyBar key's value (the API
// token); the URLs are the APIs', which tests change.
type Syncer struct {
	Token     func(name string) ([]byte, error)
	GitHubURL string // https://api.github.com
	VercelURL string // https://api.vercel.com
	HTTP      *http.Client
	// GH runs `gh secret set` when there is no GitHub token key: gh's own
	// login. nil: look for gh in PATH.
	GH func(ctx context.Context, args []string, stdin []byte) error
}

// TokenKey is the key holding the platform's API token.
func TokenKey(p vault.Platform) string {
	if p.TokenKey != "" {
		return p.TokenKey
	}
	if p.Kind == "vercel" {
		return VercelTokenKey
	}
	return GitHubTokenKey
}

// Update gives the platform the key's value.
func (s *Syncer) Update(ctx context.Context, key string, p vault.Platform, value []byte) error {
	switch p.Kind {
	case "github":
		return s.github(ctx, key, p, value)
	case "vercel":
		return s.vercel(ctx, key, p, value)
	}
	return fmt.Errorf("%s 只能手动更新", vault.KindName(p.Kind))
}

func (s *Syncer) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *Syncer) token(p vault.Platform) (string, error) {
	if s.Token == nil {
		return "", vault.ErrNotFound
	}
	b, err := s.Token(TokenKey(p))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (s *Syncer) do(ctx context.Context, method, u, token string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "KeyBar")
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		msg := e.Message
		if msg == "" {
			msg = e.Error.Message
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, msg)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// github sets the repository's (or its environment's, Envs[0]) Actions
// secret: sealed with the repository's public key, as the API requires.
func (s *Syncer) github(ctx context.Context, key string, p vault.Platform, value []byte) error {
	owner, repo, ok := strings.Cut(p.Target, "/")
	if !ok || owner == "" || repo == "" {
		return fmt.Errorf("GitHub 目标应是 owner/repo：%q", p.Target)
	}
	name := p.VarName(key)
	token, err := s.token(p)
	if errors.Is(err, vault.ErrNotFound) {
		return s.ghCLI(ctx, name, p, value)
	}
	if err != nil {
		return err
	}
	base := s.GitHubURL
	if base == "" {
		base = "https://api.github.com"
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/secrets", url.PathEscape(owner), url.PathEscape(repo))
	if len(p.Envs) > 0 {
		path = fmt.Sprintf("/repos/%s/%s/environments/%s/secrets", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(p.Envs[0]))
	}
	var pk struct {
		KeyID string `json:"key_id"`
		Key   string `json:"key"`
	}
	if err := s.do(ctx, "GET", base+path+"/public-key", token, nil, &pk); err != nil {
		return fmt.Errorf("读取仓库公钥：%w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(pk.Key)
	if err != nil || len(raw) != 32 {
		return errors.New("仓库公钥格式不对")
	}
	var pub [32]byte
	copy(pub[:], raw)
	sealed, err := box.SealAnonymous(nil, value, &pub, rand.Reader)
	if err != nil {
		return err
	}
	body := map[string]string{"encrypted_value": base64.StdEncoding.EncodeToString(sealed), "key_id": pk.KeyID}
	if err := s.do(ctx, "PUT", base+path+"/"+url.PathEscape(name), token, body, nil); err != nil {
		return fmt.Errorf("更新 %s：%w", name, err)
	}
	return nil
}

func (s *Syncer) ghCLI(ctx context.Context, name string, p vault.Platform, value []byte) error {
	args := []string{"secret", "set", name, "--repo", p.Target}
	if len(p.Envs) > 0 {
		args = append(args, "--env", p.Envs[0])
	}
	if s.GH != nil {
		return s.GH(ctx, args, value)
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("没有 GitHub 令牌：用 keys add %s 存一个（需要 repo 权限），或安装并登录 gh", GitHubTokenKey)
	}
	cmd := exec.CommandContext(ctx, gh, args...)
	cmd.Stdin = bytes.NewReader(value) // never in argv
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh secret set：%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// vercel upserts the project's environment variable, encrypted, for its
// environments (Envs; production, preview and development by default).
func (s *Syncer) vercel(ctx context.Context, key string, p vault.Platform, value []byte) error {
	if p.Target == "" {
		return errors.New("Vercel 目标应是项目名或项目 ID")
	}
	token, err := s.token(p)
	if errors.Is(err, vault.ErrNotFound) {
		return fmt.Errorf("没有 Vercel 令牌：在 vercel.com/account/tokens 生成后用 keys add %s 存入", TokenKey(p))
	}
	if err != nil {
		return err
	}
	base := s.VercelURL
	if base == "" {
		base = "https://api.vercel.com"
	}
	q := url.Values{"upsert": {"true"}}
	if p.Team != "" {
		q.Set("teamId", p.Team)
	}
	envs := p.Envs
	if len(envs) == 0 {
		envs = []string{"production", "preview", "development"}
	}
	body := map[string]any{"key": p.VarName(key), "value": string(value), "type": "encrypted", "target": envs}
	u := base + "/v10/projects/" + url.PathEscape(p.Target) + "/env?" + q.Encode()
	if err := s.do(ctx, "POST", u, token, body, nil); err != nil {
		return fmt.Errorf("更新 %s：%w", p.VarName(key), err)
	}
	return nil
}
