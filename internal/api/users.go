package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GetMyUser fetches the current user's account information from GET /users/me.
func (c *Client) GetMyUser(ctx context.Context, _ bool) (map[string]interface{}, error) {
	u, err := c.resolveURL("users/me")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := c.getResource(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SendPhoneVerification initiates phone verification for the given user.
// Returns the verification session ID (sid) needed for VerifyPhone.
func (c *Client) SendPhoneVerification(ctx context.Context, userID, phoneNumber, channel string) (string, error) {
	u, err := c.baseURLWithSegments("users", userID, "phonenumber")
	if err != nil {
		return "", fmt.Errorf("send phone verification: %w", err)
	}
	body, err := json.Marshal(map[string]string{"phone_number": phoneNumber, "channel": channel})
	if err != nil {
		return "", fmt.Errorf("send phone verification: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("send phone verification: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send phone verification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("send phone verification: server returned %d", resp.StatusCode)
	}
	var result struct {
		SID string `json:"sid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("send phone verification: decode response: %w", err)
	}
	return result.SID, nil
}

// CheckVerificationStatus posts to /me/verification?force_refresh=1 and returns an error
// if the user's verification type is still "phone" (i.e., phone verification did not complete).
func (c *Client) CheckVerificationStatus(ctx context.Context) error {
	u, err := c.resolveURL("me/verification")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("force_refresh", "1")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Type == "phone" {
		return fmt.Errorf("phone verification status is still pending")
	}
	return nil
}

// VerifyPhone confirms the verification code for the given user and session ID.
func (c *Client) VerifyPhone(ctx context.Context, userID, sid, code string) error {
	u, err := c.baseURLWithSegments("users", userID, "phonenumber", sid)
	if err != nil {
		return fmt.Errorf("verify phone: %w", err)
	}
	body, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return fmt.Errorf("verify phone: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("verify phone: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("verify phone: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("verify phone: server returned %d", resp.StatusCode)
	}
	return nil
}
