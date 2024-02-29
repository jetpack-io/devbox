package plugin

import (
	"cmp"
	"io"
	"net/http"
	"net/url"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cachehash"
)

type githubPlugin struct {
	ref RefLike
}

func (p *githubPlugin) Fetch() ([]byte, error) {
	return p.FileContent(p.ref.withFilename(p.ref.Dir))
}

func (p *githubPlugin) CanonicalName() string {
	return p.ref.Owner + "-" + p.ref.Repo
}

func (p *githubPlugin) Hash() string {
	h, _ := cachehash.Bytes([]byte(p.CanonicalName()))
	return h
}

func (p *githubPlugin) FileContent(subpath string) ([]byte, error) {
	contentURL, err := p.url(subpath)
	if err != nil {
		return nil, err
	}

	res, err := http.Get(contentURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, usererr.New(
			"failed to get plugin github:%s (Status code %d). \nPlease make sure a "+
				"plugin.json file exists in plugin directory.",
			p.ref.String(),
			res.StatusCode,
		)
	}
	return io.ReadAll(res.Body)
}

func (p *githubPlugin) url(subpath string) (string, error) {
	// Github redirects "master" to "main" in new repos. They don't do the reverse
	// so setting master here is better.
	return url.JoinPath(
		"https://raw.githubusercontent.com/",
		p.ref.Owner,
		p.ref.Repo,
		cmp.Or(p.ref.Rev, p.ref.Ref.Ref, "master"),
		p.ref.Dir,
		subpath,
	)
}
