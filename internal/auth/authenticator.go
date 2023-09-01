// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package auth

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cuecfg"
	"go.jetpack.io/devbox/internal/envir"
)

// Authenticator performs various auth0 login flows to authenticate users.
type Authenticator struct {
	ClientID string
	Domain   string
	Scope    string
	Audience string
}

// NewAuthenticator creates an authenticator that uses the auth0 production tenancy.
func NewAuthenticator() *Authenticator {
	return &Authenticator{
		ClientID: envir.GetValueOrDefault(
			"DEVBOX_AUTH_CLIENT_ID",
			"5PusB4fMm6BQ8WbTFObkTI0JUDi9ahPC",
		),
		Domain: envir.GetValueOrDefault(
			"DEVBOX_AUTH_DOMAIN",
			"auth.jetpack.io",
		),
		Scope: envir.GetValueOrDefault(
			"DEVBOX_AUTH_SCOPE",
			"openid offline_access email profile",
		),
		Audience: envir.GetValueOrDefault(
			"DEVBOX_AUTH_AUDIENCE",
			"https://api.jetpack.io",
		),
	}
}

// DeviceAuthFlow starts decide auth flow
func (a *Authenticator) DeviceAuthFlow(ctx context.Context, w io.Writer) error {
	resp, err := a.requestDeviceCode()
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "\nYour auth code is: %s\n\n", resp.UserCode)
	a.showVerificationURL(resp.VerificationURIComplete, w)

	tokenSuccess, err := a.requestTokens(ctx, resp)
	if err != nil {
		return err
	}

	if err = cuecfg.WriteFile(getAuthFilePath(), tokenSuccess); err != nil {
		return err
	}

	fmt.Fprintln(w, "You are now authenticated.")
	return nil
}

// Use existing refresh tokens to cycle all tokens. This will fail if refresh
// tokens are missing or expired. Handle accordingly
func (a *Authenticator) RefreshTokens() (*tokenSet, error) {
	tokens := &tokenSet{}
	if err := cuecfg.ParseFile(getAuthFilePath(), tokens); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil,
				usererr.New("You must have previously logged in to use this command")
		}
		return nil, err
	}

	tokens, err := a.doRefreshToken(tokens.RefreshToken)
	if err != nil {
		if errors.Is(err, errExpiredOrInvalidRefreshToken) {
			return nil, usererr.New("Your refresh token is expired or invalid. " +
				"Please log in again using `devbox auth login`")
		}
		return nil, err
	}

	return tokens, cuecfg.WriteFile(getAuthFilePath(), tokens)
}

func (a *Authenticator) Logout() error {
	return os.Remove(getAuthFilePath())
}
