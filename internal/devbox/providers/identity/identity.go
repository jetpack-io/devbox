package identity

import (
	"context"
	"os"

	"go.jetify.com/typeid"
	"go.jetpack.io/devbox/internal/build"
	"go.jetpack.io/pkg/api"
	"go.jetpack.io/pkg/auth"
	"go.jetpack.io/pkg/auth/session"
	"go.jetpack.io/pkg/ids"
	"golang.org/x/oauth2"
)

var scopes = []string{"openid", "offline_access", "email", "profile"}

type Provider struct {
	cachedAccessTokenFromAPIToken *session.Token
}

var singleton *Provider = &Provider{}

func Get() *Provider {
	return singleton
}

func (p *Provider) GenSession(ctx context.Context) (*session.Token, error) {
	if t, err := p.getAccessTokenFromAPIToken(ctx); err != nil || t != nil {
		return t, err
	}

	c, err := p.AuthClient()
	if err != nil {
		return nil, err
	}
	return c.GetSession(ctx)
}

func (p *Provider) Peek() (*session.Token, error) {
	if p.cachedAccessTokenFromAPIToken != nil {
		return p.cachedAccessTokenFromAPIToken, nil
	}

	c, err := p.AuthClient()
	if err != nil {
		return nil, err
	}
	tokens, err := c.GetSessions()
	if err != nil {
		return nil, err
	}

	if len(tokens) == 0 {
		return nil, auth.ErrNotLoggedIn
	}

	return tokens[0].Peek(), nil
}

func (p *Provider) AuthClient() (*auth.Client, error) {
	return auth.NewClient(
		build.Issuer(),
		build.ClientID(),
		scopes,
		build.SuccessRedirect(),
		build.Audience(),
	)
}

func (p *Provider) getAccessTokenFromAPIToken(
	ctx context.Context,
) (*session.Token, error) {
	apiTokenRaw := os.Getenv("DEVBOX_API_TOKEN")
	if apiTokenRaw == "" {
		return nil, nil
	}

	apiToken, err := typeid.Parse[ids.APIToken](apiTokenRaw)
	if err != nil {
		return nil, err
	}

	apiClient := api.NewClient(ctx, build.JetpackAPIHost(), &session.Token{})
	response, err := apiClient.GetAccessToken(ctx, apiToken)
	if err != nil {
		return nil, err
	}

	// This is not the greatest. This token is missing id, refresh, etc.
	// It may be better to change api.NewClient() to take a token string instead.
	p.cachedAccessTokenFromAPIToken = &session.Token{
		Token: oauth2.Token{
			AccessToken: response.AccessToken,
		},
	}

	return p.cachedAccessTokenFromAPIToken, nil
}
