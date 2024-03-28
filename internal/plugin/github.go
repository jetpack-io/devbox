package plugin

import (
	"cmp"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cachehash"
	"go.jetpack.io/devbox/nix/flake"
)

type githubPlugin struct {
	ref  flake.Ref
	name string
}

// Github only allows alphanumeric, hyphen, underscore, and period in repo names.
// but we clean up just in case.
var githubNameRegexp = regexp.MustCompile("[^a-zA-Z0-9-_.]+")

func newGithubPlugin(ref flake.Ref) (*githubPlugin, error) {
	plugin := &githubPlugin{ref: ref}
	// For backward compatibility, we don't strictly require name to be present
	// in github plugins. If it's missing, we just use the directory as the name.
	name, err := getPluginNameFromContent(plugin)
	if err != nil && !errors.Is(err, errNameMissing) {
		return nil, err
	}
	if name == "" {
		name = strings.ReplaceAll(ref.Dir, "/", "-")
	}
	plugin.name = githubNameRegexp.ReplaceAllString(
		strings.Join(lo.Compact([]string{ref.Owner, ref.Repo, name}), "."),
		" ",
	)
	return plugin, nil
}

func (p *githubPlugin) Fetch() ([]byte, error) {
	content, err := p.FileContent(pluginConfigName)
	if err != nil {
		return nil, err
	}
	return jsonPurifyPluginContent(content)
}

func (p *githubPlugin) CanonicalName() string {
	return p.name
}

func (p *githubPlugin) Hash() string {
	h, _ := cachehash.Bytes([]byte(p.ref.String()))
	return h
}

func (p *githubPlugin) FileContent(subpath string) ([]byte, error) {
	req, err := p.request(subpath)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, usererr.New(
			"failed to get plugin %s @ %s (Status code %d). \nPlease make "+
				"sure a plugin.json file exists in plugin directory.",
			p.LockfileKey(),
			req.URL.String(),
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
		cmp.Or(p.ref.Rev, p.ref.Ref, "master"),
		p.ref.Dir,
		subpath,
	)
}

func (p *githubPlugin) request(subpath string) (*http.Request, error) {
	contentURL, err := p.url(subpath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, contentURL, nil)
	if err != nil {
		return nil, err
	}

	// Add github token to request if available
	ghToken := os.Getenv("GITHUB_TOKEN")

	if ghToken != "" {
		authValue := fmt.Sprintf("token %s", ghToken)
		req.Header.Add("Authorization", authValue)
	}

	return req, nil
}

func (p *githubPlugin) LockfileKey() string {
	return p.ref.String()
}
