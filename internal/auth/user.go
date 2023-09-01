// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package auth

import (
	"fmt"
	"os"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cuecfg"
)

type User struct {
	filesystemTokens *tokenSet
	IDToken          *jwt.Token
}

func GetUser() (*User, error) {
	filesystemTokens := &tokenSet{}
	if err := cuecfg.ParseFile(getAuthFilePath(), filesystemTokens); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, usererr.New(
				"You must be logged in to use this command. Run `devbox auth login`",
			)
		}
		return nil, err
	}
	// Attempt to parse and verify the ID token.
	IDToken, err := parseToken(filesystemTokens.IDToken)
	if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
		return nil, err
	}

	// If the token is expired, refresh the tokens and try again.
	if errors.Is(err, jwt.ErrTokenExpired) {
		filesystemTokens, err = NewAuthenticator().RefreshTokens()
		if err != nil {
			return nil, err
		}
		IDToken, err = parseToken(filesystemTokens.IDToken)
		if err != nil {
			return nil, err
		}
	}

	return &User{filesystemTokens: filesystemTokens, IDToken: IDToken}, nil
}

func (u *User) String() string {
	return u.Email()
}

func (u *User) Email() string {
	if u == nil || u.IDToken == nil {
		return ""
	}
	return u.IDToken.Claims.(jwt.MapClaims)["email"].(string)
}

func (u *User) ID() string {
	if u == nil || u.IDToken == nil {
		return ""
	}
	return u.IDToken.Claims.(jwt.MapClaims)["sub"].(string)
}

func parseToken(stringToken string) (*jwt.Token, error) {
	authenticator := NewAuthenticator()
	jwksURL := fmt.Sprintf(
		"https://%s/.well-known/jwks.json",
		authenticator.Domain,
	)
	// TODO: Cache this
	jwks, err := keyfunc.Get(jwksURL, keyfunc.Options{})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	token, err := jwt.Parse(stringToken, jwks.Keyfunc)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return token, nil
}
