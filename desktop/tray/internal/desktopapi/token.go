package desktopapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

const (
	BridgeTokenEnv = "DAGENTS_DESKTOP_BRIDGE_TOKEN"
	BridgeURLEnv   = "DAGENTS_DESKTOP_API_URL"
)

// EnsureBridgeToken creates the per-shell secret used by Node to call the
// private Shell bridge. A new token is written on every Shell start; the
// standalone `dagents-shell update` command reads the token of the currently
// running Shell through ReadBridgeToken.
func EnsureBridgeToken(path string) (string, error) {
	if token := strings.TrimSpace(os.Getenv(BridgeTokenEnv)); token != "" {
		// A parent Shell process may already have a token in its environment.
		// Reuse it so all components of this process share one secret.
		return token, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate desktop bridge token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return "", fmt.Errorf("create desktop bridge token directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write desktop bridge token: %w", err)
	}
	if err := os.Setenv(BridgeTokenEnv, token); err != nil {
		return "", err
	}
	return token, nil
}

func ReadBridgeToken(path string) string {
	if token := strings.TrimSpace(os.Getenv(BridgeTokenEnv)); token != "" {
		return token
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// filepathDir is kept local to avoid exposing filesystem layout through the
// desktop API package's public surface.
func filepathDir(path string) string {
	idx := strings.LastIndexAny(path, `/\\`)
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return path[:1]
	}
	return path[:idx]
}
