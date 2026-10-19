// Package twitter provides an OAuth 2.0 PKCE client for the Twitter v2 API.
package twitter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/store"
)

const (
	twitterAuthURL  = "https://twitter.com/i/oauth2/authorize"
	twitterTokenURL = "https://api.twitter.com/2/oauth2/token"
	twitterAPIBase  = "https://api.twitter.com/2"

	// pkceStateTTL is how long a pending OAuth state is kept in memory.
	pkceStateTTL = 10 * time.Minute
)

// pkceState is a pending OAuth 2.0 PKCE authorisation.
type pkceState struct {
	verifier  string
	expiresAt time.Time
}

// Client handles Twitter OAuth 2.0 PKCE and v2 API calls.
type Client struct {
	clientID     string
	clientSecret string
	callbackURL  string
	httpClient   *http.Client
	st           store.Store

	mu     sync.Mutex
	states map[string]pkceState // key = state token
}

// NewClient creates a Twitter Client.
// clientID is the Twitter OAuth 2.0 app client ID.
// clientSecret is required for confidential clients (empty for public clients).
// callbackURL is the registered redirect URI.
func NewClient(clientID, clientSecret, callbackURL string, st store.Store) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		callbackURL:  callbackURL,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		st:           st,
		states:       make(map[string]pkceState),
	}
}

// StartAuth generates a PKCE challenge and returns the Twitter authorization URL.
// The state token is stored in memory (with a 10-minute TTL) to validate the callback.
func (c *Client) StartAuth() (authURL string, state string, err error) {
	verifier, err := randomBase64(32)
	if err != nil {
		return "", "", fmt.Errorf("twitter: pkce verifier: %w", err)
	}
	state, err = randomBase64(16)
	if err != nil {
		return "", "", fmt.Errorf("twitter: pkce state: %w", err)
	}

	// S256 challenge: BASE64URL(SHA256(verifier))
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	c.mu.Lock()
	c.purgeExpiredStates()
	c.states[state] = pkceState{verifier: verifier, expiresAt: time.Now().Add(pkceStateTTL)}
	c.mu.Unlock()

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.clientID},
		"redirect_uri":          {c.callbackURL},
		"scope":                 {"tweet.read tweet.write like.read like.write users.read offline.access"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authURL = twitterAuthURL + "?" + params.Encode()
	return authURL, state, nil
}

// HandleCallback exchanges the authorisation code for tokens and persists them.
func (c *Client) HandleCallback(ctx context.Context, code, state string) error {
	c.mu.Lock()
	ps, ok := c.states[state]
	if ok {
		delete(c.states, state)
	}
	c.mu.Unlock()

	if !ok || time.Now().After(ps.expiresAt) {
		return fmt.Errorf("twitter: invalid or expired state token")
	}

	tokens, err := c.exchangeCode(ctx, code, ps.verifier)
	if err != nil {
		return fmt.Errorf("twitter: exchange code: %w", err)
	}

	me, err := c.fetchMe(ctx, tokens.AccessToken)
	if err != nil {
		return fmt.Errorf("twitter: fetch user: %w", err)
	}

	creds := &model.TwitterCredentials{
		AccessToken:   tokens.AccessToken,
		RefreshToken:  tokens.RefreshToken,
		ExpiresAt:     time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
		TwitterUserID: me.ID,
		Username:      me.Username,
		UpdatedAt:     time.Now(),
	}
	return c.st.SaveTwitterCredentials(ctx, creds)
}

// Status returns the connected account info, or nil if not connected.
func (c *Client) Status(ctx context.Context) (*model.TwitterCredentials, error) {
	creds, err := c.st.GetTwitterCredentials(ctx)
	if err != nil {
		if isSQLNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return creds, nil
}

// Disconnect removes stored credentials.
func (c *Client) Disconnect(ctx context.Context) error {
	return c.st.DeleteTwitterCredentials(ctx)
}

// Retweet retweeets tweetID on behalf of the authenticated user.
func (c *Client) Retweet(ctx context.Context, tweetID string) (string, error) {
	creds, err := c.credentials(ctx)
	if err != nil {
		return "", err
	}

	payload := map[string]string{"tweet_id": tweetID}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		fmt.Sprintf("%s/users/%s/retweets", twitterAPIBase, creds.TwitterUserID),
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("twitter: retweet: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	var result struct {
		Data struct {
			Retweeted bool `json:"retweeted"`
		} `json:"data"`
	}
	if err := c.doJSON(req, &result); err != nil {
		return "", fmt.Errorf("twitter: retweet: %w", err)
	}
	return tweetID, nil
}

// Like likes tweetID on behalf of the authenticated user.
func (c *Client) Like(ctx context.Context, tweetID string) error {
	creds, err := c.credentials(ctx)
	if err != nil {
		return err
	}

	payload := map[string]string{"tweet_id": tweetID}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		fmt.Sprintf("%s/users/%s/likes", twitterAPIBase, creds.TwitterUserID),
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("twitter: like: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	return c.doJSON(req, nil)
}

// QuoteTweet posts a quote tweet of tweetID with the given text.
// Returns the new tweet ID.
func (c *Client) QuoteTweet(ctx context.Context, tweetID, text string) (string, error) {
	creds, err := c.credentials(ctx)
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"text":           text,
		"quote_tweet_id": tweetID,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		twitterAPIBase+"/tweets",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("twitter: quote tweet: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.doJSON(req, &result); err != nil {
		return "", fmt.Errorf("twitter: quote tweet: %w", err)
	}
	return result.Data.ID, nil
}

// credentials returns valid credentials, refreshing the access token if needed.
func (c *Client) credentials(ctx context.Context) (*model.TwitterCredentials, error) {
	creds, err := c.st.GetTwitterCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("twitter: no credentials stored: %w", err)
	}

	// Refresh if expiring within 5 minutes.
	if time.Until(creds.ExpiresAt) < 5*time.Minute {
		refreshed, err := c.refreshToken(ctx, creds.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("twitter: refresh token: %w", err)
		}
		creds.AccessToken = refreshed.AccessToken
		creds.RefreshToken = refreshed.RefreshToken
		creds.ExpiresAt = time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
		creds.UpdatedAt = time.Now()
		if err := c.st.SaveTwitterCredentials(ctx, creds); err != nil {
			return nil, fmt.Errorf("twitter: save refreshed credentials: %w", err)
		}
	}
	return creds, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (c *Client) exchangeCode(ctx context.Context, code, verifier string) (*tokenResponse, error) {
	form := url.Values{
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"client_id":     {c.clientID},
		"redirect_uri":  {c.callbackURL},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitterTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.clientSecret != "" {
		req.SetBasicAuth(c.clientID, c.clientSecret)
	}

	var tok tokenResponse
	if err := c.doJSON(req, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (c *Client) refreshToken(ctx context.Context, refreshToken string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.clientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitterTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.clientSecret != "" {
		req.SetBasicAuth(c.clientID, c.clientSecret)
	}

	var tok tokenResponse
	if err := c.doJSON(req, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

type twitterUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (c *Client) fetchMe(ctx context.Context, accessToken string) (*twitterUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		twitterAPIBase+"/users/me?user.fields=username", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	var result struct {
		Data twitterUser `json:"data"`
	}
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// RateLimitError is returned when Twitter responds with HTTP 429.
type RateLimitError struct {
	ResetAt time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("twitter: rate limited (reset at %s)", e.ResetAt.Format(time.RFC3339))
}

func (c *Client) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		var resetAt time.Time
		if v := resp.Header.Get("x-rate-limit-reset"); v != "" {
			// Unix timestamp
			if ts, err := parseInt64(v); err == nil {
				resetAt = time.Unix(ts, 0)
			}
		}
		return &RateLimitError{ResetAt: resetAt}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("twitter: HTTP %d: %s", resp.StatusCode, string(body))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) purgeExpiredStates() {
	now := time.Now()
	for k, v := range c.states {
		if now.After(v.expiresAt) {
			delete(c.states, k)
		}
	}
}

func randomBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isSQLNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) ||
		(err != nil && strings.Contains(err.Error(), "not found"))
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}
