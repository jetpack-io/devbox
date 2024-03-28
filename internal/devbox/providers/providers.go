package providers

import (
	"go.jetpack.io/devbox/internal/devbox/providers/identity"
	"go.jetpack.io/devbox/internal/devbox/providers/nixcache"
)

// Providers is a struct that contains all the providers that devbox uses.
// A provider encapsulates data and/or logic that can affect devbox behavior.
// What a provider does can be influenced by the environment or external services.
// The goal is to centralize this logic and avoid conditionals in core devbox code.
// In the future we should allow dynamic providers as well which can help
// customize devbox behavior.
//
// Providers should have
// 1) A default behavior
// 2) A way to override that behavior (e.g. through environment variables, logged in state, etc.)
type Providers struct {
	NixCache nixcache.Provider
	Identity identity.Provider
}
