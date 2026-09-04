//go:build !windows

package desktopapi

import "errors"

func pickDirectory() (string, error) {
	return "", errors.New("native directory picker is unavailable")
}
