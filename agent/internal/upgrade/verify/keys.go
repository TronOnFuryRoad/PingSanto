package verify

import (
	_ "embed"
	"strings"
)

//go:embed keys/pingsanto-agent.pub
var embeddedPublicKey string

// DefaultPublicKey returns the embedded Minisign public key used to verify agent upgrade artifacts.
func DefaultPublicKey() string {
	return strings.TrimSpace(embeddedPublicKey)
}
