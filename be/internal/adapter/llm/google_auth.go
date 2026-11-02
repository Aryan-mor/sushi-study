package llm

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type saFile struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

type tokenSource struct {
	email, keyPEM, tokenURI string
	http                    *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func newTokenSource(raw string, httpClient *http.Client) (*tokenSource, error) {
	sa, err := parseServiceAccount(raw)
	if err != nil {
		return nil, err
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &tokenSource{email: sa.ClientEmail, keyPEM: sa.PrivateKey, tokenURI: sa.TokenURI, http: httpClient}, nil
}

// parseServiceAccount accepts compact JSON, or the same bytes after Docker env_file left
// dotenv quotes/backslashes in the value (engine does not unescape env_file).
func parseServiceAccount(raw string) (saFile, error) {
	var last error
	for _, candidate := range saJSONCandidates(raw) {
		var sa saFile
		if err := json.Unmarshal([]byte(candidate), &sa); err != nil {
			last = err
			continue
		}
		if sa.ClientEmail == "" || sa.PrivateKey == "" {
			last = fmt.Errorf("missing client_email/private_key")
			continue
		}
		return sa, nil
	}
	if last == nil {
		last = fmt.Errorf("empty")
	}
	return saFile{}, fmt.Errorf("llm: google: service account json: %w", last)
}

func saJSONCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	out := []string{raw}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if u, err := strconv.Unquote(raw); err == nil {
			out = append(out, u)
		}
	}
	if u, err := strconv.Unquote(`"` + raw + `"`); err == nil {
		out = append(out, u)
	}
	return out
}

func (s *tokenSource) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.expires.Add(-60*time.Second)) {
		return s.token, nil
	}
	tok, exp, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token, s.expires = tok, exp
	return tok, nil
}

func (s *tokenSource) fetch(ctx context.Context) (string, time.Time, error) {
	jwt, err := googleJWT(s.email, s.keyPEM, s.tokenURI)
	if err != nil {
		return "", time.Time{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("google: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("google: token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("google: token read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("google: token http %d: %s", res.StatusCode, truncate(body, 200))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("google: token decode")
	}
	sec := parsed.ExpiresIn
	if sec <= 0 {
		sec = 3600
	}
	return parsed.AccessToken, time.Now().Add(time.Duration(sec) * time.Second), nil
}

func googleJWT(email, keyPEM, aud string) (string, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return "", fmt.Errorf("google: pem decode private_key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("google: parse private_key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("google: private_key is not RSA")
	}
	now := time.Now().Unix()
	header := b64url([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Iat   int64  `json:"iat"`
		Exp   int64  `json:"exp"`
	}{Iss: email, Scope: vertexScope, Aud: aud, Iat: now, Exp: now + 3600})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + b64url(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("google: jwt sign: %w", err)
	}
	return unsigned + "." + b64url(sig), nil
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
