package nix

import (
	"context"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/pkg/errors"
)

const searchURL = `https://search.nixos.org/packages?channel=22.11&from=0&size=10&sort=relevance&type=packages&query=%s`

type searchResult struct {
	Name    string
	Version string
}

func Search(ctx context.Context, query string) ([]searchResult, error) {
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	var packageNodes []*cdp.Node
	var versionNodes []*cdp.Node
	err := chromedp.Run(
		ctx,
		chromedp.Navigate(fmt.Sprintf(searchURL, query)),
		chromedp.Nodes(`.package`, &packageNodes, chromedp.NodeVisible),
		chromedp.Nodes(`.package ul li:nth-child(2) strong`, &versionNodes),
	)

	if err != nil {
		return nil, errors.WithStack(err)
	}

	results := []searchResult{}
	for idx, node := range packageNodes {
		results = append(results, searchResult{
			Name:    strings.Split(node.AttributeValue("id"), "-")[1],
			Version: versionNodes[idx].Children[0].NodeValue,
		})
	}
	return results, nil
}
