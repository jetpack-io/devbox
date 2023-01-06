package nix

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/debug"
)

var (
	//go:embed indexpkgs.nix
	nixExprIndexPkgs     string
	nixExprIndexPkgsTmpl = template.Must(template.New("indexpkgs.nix").Parse(nixExprIndexPkgs))
)

type PkgIndex struct {
	pkgs          []PackageInfo
	byAttrPath    map[string]int
	groupedByName map[string][]int

	commitHash string
}

func IndexPackages(ctx context.Context, commitHash string) (*PkgIndex, error) {
	index := &PkgIndex{
		byAttrPath:    make(map[string]int),
		groupedByName: make(map[string][]int),
		commitHash:    commitHash,
	}
	if debug.IsEnabled() {
		start := time.Now()
		defer func() {
			debug.Log("nix: package indexing stats: dur=%s, num_pkgs=%d",
				time.Since(start), len(index.pkgs))
		}()
	}

	cache, err := newPkgIndexCache()
	if err != nil {
		return nil, err
	}
	pkgJSON, err := cache.Get(commitHash)
	if err != nil {
		debug.Log("nix: cache miss for commit %s: %v", commitHash, err)
		pkgJSON, err = index.runNix(ctx, commitHash)
		if err != nil {
			return nil, err
		}
		if err := cache.Put(commitHash, pkgJSON); err != nil {
			return nil, err
		}
	}

	// We happen to know that the number of packages in nixpkgs is currently
	// 41,699, so we can preallocate the slice. A future optimization could
	// have Nix output the exact number with the JSON.
	index.pkgs = make([]PackageInfo, 0, 50_000)
	dec := json.NewDecoder(bytes.NewReader(pkgJSON))

	// Consume opening brace for top-level object.
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	// Iterate through top-level object properties.
	for i := 0; dec.More(); i++ {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		index.pkgs = append(index.pkgs, PackageInfo{
			AttrPath: ParseAttrPath(key.(string)),
		})
		pkg := &index.pkgs[len(index.pkgs)-1]
		if err := dec.Decode(pkg); err != nil {
			return nil, err
		}
		index.byAttrPath[pkg.AttrPath.String()] = i
		group := pkg.AttrPath.Parent().String() + "." + pkg.Name()
		index.groupedByName[group] = append(index.groupedByName[group], i)
	}
	return index, nil
}

func (s *PkgIndex) Exact(attrPath string) (PackageInfo, error) {
	if i, ok := s.byAttrPath[attrPath]; ok {
		return s.pkgs[i], nil
	}
	return PackageInfo{}, fmt.Errorf("nix: nixpkgs@%7s doesn't have a package named %q",
		s.commitHash, attrPath)
}

func (s *PkgIndex) Search(pattern string) []PackageInfo {
	type pkgScore struct {
		score float64
		pkg   *PackageInfo
	}

	patternAP := ParseAttrPath(pattern)
	scored := make([]pkgScore, len(s.pkgs))
	for i := range s.pkgs {
		candidate := &s.pkgs[i] // don't allocate a copy
		scored[i] = pkgScore{
			score: candidate.AttrPath.Match(patternAP),
			pkg:   candidate,
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	const maxResults = 20
	seen := make(map[string]bool)
	results := make([]PackageInfo, 0, maxResults)
	for _, result := range scored {
		groupName := result.pkg.AttrPath.Parent().String() + "." + result.pkg.Name()
		group := s.groupedByName[groupName]
		sort.Slice(group, func(i, j int) bool {
			return s.pkgs[i].Version() > s.pkgs[j].Version()
		})
		for _, pkgi := range group {
			if seen[s.pkgs[pkgi].AttrPath.String()] {
				continue
			}
			seen[s.pkgs[pkgi].AttrPath.String()] = true
			results = append(results, s.pkgs[pkgi])
			if len(results) == maxResults {
				return results
			}
		}
	}
	return results
}

func (s *PkgIndex) runNix(ctx context.Context, commitHash string) ([]byte, error) {
	expr := &bytes.Buffer{}
	if err := nixExprIndexPkgsTmpl.Execute(expr, commitHash); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "nix", "eval", "--read-only", "--json", "--file", "-")
	cmd.Stdin = expr

	debug.Log("nix: run command %q", cmd)
	start := time.Now()
	defer func() {
		log.Printf("nix: command %q took %s", cmd, time.Since(start))
	}()
	return cmd.Output()
}

type pkgIndexCache string

func newPkgIndexCache() (pkgIndexCache, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return pkgIndexCache(filepath.Join(cacheDir, "devbox")), nil
}

func (p pkgIndexCache) Put(commitHash string, b []byte) error {
	key := p.key(commitHash)
	err := p.write(key, b)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(string(p), 0755); err == nil {
			return p.write(key, b)
		}
	}
	return err
}

func (p pkgIndexCache) write(key string, b []byte) (err error) {
	f, err := os.OpenFile(key, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}()

	w := gzip.NewWriter(f)
	if _, err := w.Write(b); err != nil {
		return err
	}
	return w.Close()
}

func (p pkgIndexCache) Get(commitHash string) ([]byte, error) {
	key := p.key(commitHash)
	f, err := os.OpenFile(key, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (p pkgIndexCache) key(commitHash string) string {
	return filepath.Join(string(p), commitHash+".gz")
}

type AttrPath struct {
	Value  string
	tokens []string
}

func ParseAttrPath(attrPath string) AttrPath {
	ap := AttrPath{Value: attrPath}
	tokStart := -1
	for i, r := range attrPath {
		if isSep(byte(r)) {
			if tokStart != -1 {
				ap.tokens = append(ap.tokens, attrPath[tokStart:i])
				tokStart = -1
			}
		} else if tokStart == -1 {
			tokStart = i
		} else if isBound(attrPath[i-1], byte(r)) {
			ap.tokens = append(ap.tokens, attrPath[tokStart:i])
			tokStart = i
		}
	}
	if tokStart != -1 {
		ap.tokens = append(ap.tokens, attrPath[tokStart:])
	}
	return ap
}

func isSep(b byte) bool {
	return b == '_' || b == '.' || b == ' ' || b == '-'
}

func isUpper(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isBound(b1, b2 byte) bool {
	return (isDigit(b1) != isDigit(b2)) ||
		(!isUpper(b1) && isUpper(b2))
}

func (a AttrPath) Parent() AttrPath {
	i := strings.LastIndexByte(a.Value, '.')
	if i == -1 {
		return AttrPath{Value: "."}
	}
	return AttrPath{Value: a.Value[:i]}
}

func (a AttrPath) String() string {
	return a.Value
}

func (a AttrPath) Match(pattern AttrPath) float64 {
	debug.Log("nix: matching attribute path %v against the pattern %v",
		a.tokens, pattern.tokens)
	startIndex := 0
	totalScore := float64(0)
	for _, patternToken := range pattern.tokens {
		bestIndex := 0
		bestScore := float64(0)
		debug.Log("nix: matching attribute path tokens %v against the pattern token %q",
			a.tokens[startIndex:], patternToken)
		for i, token := range a.tokens[startIndex:] {
			score := scoreTokens(patternToken, token)

			// Penalty modifier to matching further to the left.
			// For example, we want the pattern "python" to match
			// higher with "python3Full" vs. "python31Packages.numpy".
			score *= (float64(startIndex) / float64(len(a.tokens))) + 1
			if score > bestScore {
				bestScore = score
				bestIndex = i
			}
		}
		debug.Log("nix: attribute path token %q has the best match with the pattern token %q (score = %.2f, index = %d)",
			a.tokens[bestIndex], patternToken, bestScore, bestIndex)

		totalScore += bestScore
		startIndex = bestIndex + 1
		if startIndex == len(a.tokens) {
			break
		}
	}

	remove := 0
	for i := len(a.tokens) - 1; i >= startIndex; i-- {
		_, err := strconv.Atoi(a.tokens[i])
		if err == nil {
			remove++
		}
	}
	if remove > 0 {
		debug.Log("nix: not factoring in the last %d tokens of attribute path %v because they look like version numbers",
			remove, a.tokens)
	}

	maxLen := len(pattern.tokens)
	if l := len(a.tokens) - remove; l > maxLen {
		maxLen = l
	}
	finalScore := totalScore / float64(maxLen)
	debug.Log("nix: the final score for attribute path %v against the pattern %v is: %.2f",
		a.tokens, pattern.tokens, finalScore)
	return finalScore
}

func scoreTokens(pattern string, candidate string) float64 {
	modifier := 0.2
	hits := 0
	qr, pattern := popRune(pattern)
	for i, cr := range candidate {
		if qr != cr {
			continue
		}
		hits++

		// Bonus for match the first character.
		if i == 0 {
			modifier += 0.2
		}
		// Bonus for a perfect match.
		if hits == len(pattern) {
			modifier += 0.2
		}

		qr, pattern = popRune(pattern)
		if qr == utf8.RuneError {
			break
		}
	}
	maxLen := len(candidate)
	if l := len(pattern); l > maxLen {
		maxLen = l
	}
	return (float64(hits) / float64(maxLen)) * modifier
}

func popRune(s string) (rune, string) {
	r, size := utf8.DecodeRuneInString(s)
	return r, s[size:]
}

type PackageInfo struct {
	AttrPath        AttrPath
	RawName         string   `json:"name"`
	Pname           string   `json:"pname"`
	RawVersion      string   `json:"version"`
	NixpkgsVersion  string   `json:"nixpkgs_version"`
	Description     string   `json:"description"`
	LongDescription string   `json:"long_description"`
	Homepage        string   `json:"homepage"`
	License         string   `json:"license"`
	Platforms       []string `json:"platforms"`
}

func (p *PackageInfo) Name() string {
	if p.Pname != "" {
		return p.Pname
	}
	if p.RawVersion != "" {
		name := strings.TrimSuffix(p.RawName, p.RawVersion)
		return strings.TrimRight(name, ".-_ ")
	}
	if before, after, ok := strings.Cut(p.RawName, "-"); ok {
		after = strings.TrimRight(after, "0123456789-._ ")
		return strings.Join([]string{before, after}, "-")
	}
	return p.RawName
}

func (p *PackageInfo) Version() string {
	return p.RawVersion
}
