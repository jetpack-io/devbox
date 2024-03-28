package identity

import (
	"context"

	"go.jetpack.io/devbox/internal/build"
	"go.jetpack.io/pkg/auth"
	"go.jetpack.io/pkg/auth/session"
)

var scopes = []string{"openid", "offline_access", "email", "profile"}

type Provider struct{}

var singleton *Provider = &Provider{}

func Get() *Provider {
	return singleton
}

func (p *Provider) GenSession(ctx context.Context) (*session.Token, error) {
	c, err := p.AuthClient()
	if err != nil {
		return nil, err
	}
	return c.GetSession(ctx)
}

func (p *Provider) AuthClient() (*auth.Client, error) {
	return auth.NewClient(
		build.Issuer(),
		build.ClientID(),
		scopes,
		build.SuccessRedirect(),
	)
}
