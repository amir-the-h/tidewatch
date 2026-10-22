package x

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/amir-the-h/tidewatch/internal/source"
)

// XRule represents a single rule returned by the X Filtered Stream rules API.
type XRule struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Tag   string `json:"tag"`
}

// RuleAPI is the X Filtered Stream rules API (satisfied by *RuleManager).
type RuleAPI interface {
	AddRule(ctx context.Context, tag, value string) (string, error)
	DeleteRule(ctx context.Context, xRuleID string) error
	ListRules(ctx context.Context) ([]XRule, error)
}

// RuleManager calls the X Filtered Stream rules API with one app's bearer
// token. Stream.WithRules hands out one for the active x_stream source.
type RuleManager struct {
	bearerToken string
	httpClient  *http.Client
	apiBase     string // default: "https://api.twitter.com"
}

// rulesURL returns the full URL for the rules endpoint.
func (rm *RuleManager) rulesURL() string {
	return rm.apiBase + "/2/tweets/search/stream/rules"
}

// doRequest executes an HTTP request, sets the Authorization header, and returns the response body.
// Returns an error if the status code is not in the 2xx range.
func (rm *RuleManager) doRequest(ctx context.Context, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("x: rules: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+rm.bearerToken)
	req.Header.Set("User-Agent", source.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := rm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x: rules: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("x: rules: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("x: rules: http %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// AddRule creates a new rule on X's rules API. Returns the X-assigned rule ID.
func (rm *RuleManager) AddRule(ctx context.Context, tag, value string) (string, error) {
	payload := map[string]any{
		"add": []map[string]string{
			{"value": value, "tag": tag},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("x: rules: marshal add payload: %w", err)
	}

	respBody, err := rm.doRequest(ctx, http.MethodPost, rm.rulesURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("x: rules: unmarshal add response: %w", err)
	}
	if len(result.Data) == 0 {
		return "", fmt.Errorf("x: rules: add returned no rule IDs")
	}
	return result.Data[0].ID, nil
}

// DeleteRule removes a rule from X's rules API by X rule ID.
func (rm *RuleManager) DeleteRule(ctx context.Context, xRuleID string) error {
	payload := map[string]any{
		"delete": map[string]any{
			"ids": []string{xRuleID},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("x: rules: marshal delete payload: %w", err)
	}

	_, err = rm.doRequest(ctx, http.MethodPost, rm.rulesURL(), bytes.NewReader(body))
	return err
}

// ListRules returns all current rules from X's rules API.
func (rm *RuleManager) ListRules(ctx context.Context) ([]XRule, error) {
	respBody, err := rm.doRequest(ctx, http.MethodGet, rm.rulesURL(), nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []XRule `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("x: rules: unmarshal list response: %w", err)
	}
	return result.Data, nil
}
