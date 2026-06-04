package morpheus

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultOAuthClientID = "morph-api"

// TokenResponse is returned by POST /oauth/token.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// LoginWithPassword exchanges Morpheus username/password for an API access token.
func LoginWithPassword(baseURL, username, password string, skipTLS bool) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	username = strings.TrimSpace(username)
	password = password
	if baseURL == "" || username == "" || password == "" {
		return "", fmt.Errorf("url, username, and password are required")
	}

	q := url.Values{}
	q.Set("grant_type", "password")
	q.Set("scope", "write")
	q.Set("client_id", defaultOAuthClientID)
	q.Set("username", username)

	tokenURL := baseURL + "/oauth/token?" + q.Encode()
	body := url.Values{}
	body.Set("password", password)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLS},
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("oauth login HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var tr TokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("oauth response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return "", fmt.Errorf("oauth response missing access_token")
	}
	return tr.AccessToken, nil
}

// NewClientFromPassword builds a client after OAuth login.
func NewClientFromPassword(baseURL, username, password string, skipTLS bool) (*Client, error) {
	token, err := LoginWithPassword(baseURL, username, password, skipTLS)
	if err != nil {
		return nil, err
	}
	return NewClient(baseURL, token, skipTLS), nil
}
