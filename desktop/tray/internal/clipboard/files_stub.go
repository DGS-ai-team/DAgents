//go:build !windows

package clipboard

func filePaths() ([]string, error) {
	return nil, nil
}
