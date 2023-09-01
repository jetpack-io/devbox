// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/browser"
	"github.com/pkg/errors"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/xdg"
)

var errExpiredOrInvalidRefreshToken = errors.New("refresh token is expired or invalid")

const additionalSleepOnSlowDown = time.Second

type codeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"` // in seconds
}

type tokenSet struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// used for both requestToken and refreshToken functions
type requestTokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// showVerificationURL presents a device flow verification URL to the user,
// either by printing it to stdout or opening a web browser.
func (a *Authenticator) showVerificationURL(url string, w io.Writer) {
	err := browser.OpenURL(url)
	if err == nil {
		fmt.Fprintf(w, "Opening your browser to complete the login. "+
			"If your browser didn't open, you can go to this URL "+
			"and confirm your code manually:\n%s\n\n", url)
		return
	}
	fmt.Fprintf(
		w,
		"Please go to this URL to confirm this code and login: %s\n\n", url)
}

// requestDeviceCode requests a device code that the user can use to authorize the device.
func (a *Authenticator) requestDeviceCode() (*codeResponse, error) {
	reqURL := fmt.Sprintf("https://%s/oauth/device/code", a.Domain)
	payload := strings.NewReader(fmt.Sprintf(
		"client_id=%s&scope=%s&audience=%s",
		a.ClientID,
		url.QueryEscape(a.Scope),
		a.Audience,
	))

	req, err := http.NewRequest(http.MethodPost, reqURL, payload)
	if err != nil {
		bytesPayload, _ := io.ReadAll(payload)
		return nil, errors.Wrapf(
			err,
			"failed to send request to URL: %s with payload: %s",
			reqURL,
			string(bytesPayload),
		)
	}

	req.Header.Add("content-type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send Request")
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	if res.StatusCode != http.StatusOK {
		return nil, errors.Errorf(
			"got status code: %d, with body %s",
			res.StatusCode,
			string(body),
		)
	}

	response := codeResponse{}
	return &response, json.Unmarshal(body, &response)
}

// requestTokens polls the Auth0 API for tokens.
func (a *Authenticator) requestTokens(ctx context.Context, codeResponse *codeResponse) (*tokenSet, error) {
	timeToSleep := time.Duration(codeResponse.Interval) * time.Second
	ticker := time.NewTicker(timeToSleep)
	defer ticker.Stop()

	// numTries is a counter to guard against infinite looping.
	// In the normal course:
	//    Status Code 200 OK: we early return within loop
	//    Known Error scenarios: we continue looping and requesting Auth0 API.
	//       These are not "errors" so much as "user hasn't yet completed
	//       browser login flow"
	//    Unknown Error scenarios: we early return within loop

	for numTries := 0; numTries < 100; numTries++ {
		select {
		case <-ctx.Done():
			return nil, errors.WithStack(ctx.Err())

		case <-ticker.C:
			res, err := a.tryRequestToken(codeResponse)
			if err != nil {
				return nil, errors.WithStack(err)
			}

			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			if err != nil {
				return nil, errors.WithStack(err)
			}

			// Handle success
			if res.StatusCode == http.StatusOK {
				tokens := tokenSet{}
				return &tokens, json.Unmarshal(body, &tokens)
			}

			// Handle failure scenarios
			moreSleep, err := handleFailure(body, res.StatusCode)
			if err != nil {
				return nil, errors.WithStack(err)
			}
			timeToSleep += moreSleep
			ticker.Reset(timeToSleep)
		}
	}

	return nil, usererr.New("max number of tries exceeded")
}

func (a *Authenticator) doRefreshToken(refreshToken string) (*tokenSet, error) {
	reqURL := fmt.Sprintf("https://%s/oauth/token", a.Domain)

	payload := fmt.Sprintf(
		"grant_type=refresh_token&client_id=%s&refresh_token=%s",
		a.ClientID,
		refreshToken,
	)
	payloadReader := strings.NewReader(payload)

	req, err := http.NewRequest(http.MethodPost, reqURL, payloadReader)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"failed to create request to URL: %s, with payload: %s",
			reqURL,
			payload,
		)
	}

	req.Header.Add("content-type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"failed POST request to reqURL: %s, payload: %s ",
			reqURL,
			payload,
		)
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	if res.StatusCode == http.StatusOK {
		tokens := &tokenSet{}
		return tokens, json.Unmarshal(body, tokens)
	}

	tokenErrorBody := requestTokenError{}
	if err := json.Unmarshal(body, &tokenErrorBody); err != nil {
		return nil, errors.Wrapf(
			err,
			"unable to unmarshal requestTokenError from body %s",
			body,
		)
	}

	// Special case this, since it usually means refresh token is expired.
	if tokenErrorBody.Error == "invalid_grant" {
		return nil, errExpiredOrInvalidRefreshToken
	}

	return nil, errors.Errorf(
		"refreshing access token returned an error (%s) with description: %s",
		tokenErrorBody.Error,
		tokenErrorBody.ErrorDescription,
	)
}

func (a *Authenticator) tryRequestToken(codeResponse *codeResponse) (*http.Response, error) {
	reqURL := fmt.Sprintf("https://%s/oauth/token", a.Domain)

	grantType := "urn:ietf:params:oauth:grant-type:device_code"
	payload := strings.NewReader(fmt.Sprintf(
		"grant_type=%s&device_code=%s&client_id=%s",
		url.QueryEscape(grantType),
		codeResponse.DeviceCode,
		a.ClientID,
	))

	req, err := http.NewRequest(http.MethodPost, reqURL, payload)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	req.Header.Add("content-type", "application/x-www-form-urlencoded")

	return http.DefaultClient.Do(req)
}

// handleFailure handles the failure scenarios for requestTokens.
func handleFailure(body []byte, code int) (time.Duration, error) {
	tokenErrorBody := requestTokenError{}
	if err := json.Unmarshal(body, &tokenErrorBody); err != nil {
		return 0, errors.WithStack(err)
	}

	switch code {
	case http.StatusTooManyRequests: // 429
		if tokenErrorBody.Error != "slow_down" {
			return 0, errors.Errorf("got status code: %d, response body: %s", code, body)
		}

		return additionalSleepOnSlowDown, nil
	case http.StatusForbidden: // 403
		// this error is received when waiting for user to take action
		// when they are logging in via browser. Continue polling.
		if tokenErrorBody.Error != "authorization_pending" {
			return 0, errors.Errorf("got status code: %d, response body: %s", code, body)
		}

		return 0, nil // No slowdown, just keep trying
	}

	// The user has not authorized the device quickly enough, so
	// the `device_code` has expired. Notify the user that the
	// flow has expired and prompt them to re-initiate the flow.
	// The "expired_token" is returned exactly once. After that,
	// the dreaded "invalid_grant" will be returned and device
	// must stop polling.
	if tokenErrorBody.Error == "expired_token" || tokenErrorBody.Error == "invalid_grant" {
		return 0, usererr.New("The device code has expired. Please try `devbox auth login` again.")
	}

	// "access_denied" can be received for:
	// 1. user refused to authorize the device.
	// 2. Auth server denied the transaction.
	// 3. A configured Auth0 "rule" denied access
	if tokenErrorBody.Error == "access_denied" {
		return 0, usererr.New("Access was denied")
	}

	// Unknown error
	return 0, usererr.New("Unable to login")
}

func getAuthFilePath() string {
	return xdg.StateSubpath(filepath.FromSlash("devbox/auth.json"))
}
