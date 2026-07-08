//go:build windows

package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// installReleasePackage 解压发布包并覆盖安装根 bin/*、VERSION、dagents.cmd（对齐 dagents.cmd update PowerShell 段）。
func installReleasePackage(installHome, pkgPath string) error {
	home, err := filepath.Abs(strings.TrimSpace(installHome))
	if err != nil {
		return err
	}
	pkgPath, err = filepath.Abs(strings.TrimSpace(pkgPath))
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp("", "dagents-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if err := extractPackage(pkgPath, staging); err != nil {
		return err
	}
	bundle, err := findBundleRoot(staging)
	if err != nil {
		return err
	}
	binSrc := filepath.Join(bundle, "bin")
	if info, err := os.Stat(binSrc); err != nil || !info.IsDir() {
		return fmt.Errorf("release bundle missing bin/: %s", binSrc)
	}
	binDest := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDest, 0o755); err != nil {
		return err
	}
	if err := copyTree(binSrc, binDest); err != nil {
		return err
	}
	if src := filepath.Join(bundle, "dagents.cmd"); fileExists(src) {
		if err := copyFile(src, filepath.Join(home, "dagents.cmd")); err != nil {
			return err
		}
	}
	if src := filepath.Join(bundle, "VERSION"); fileExists(src) {
		if err := copyFile(src, filepath.Join(home, "VERSION")); err != nil {
			return err
		}
	}
	return nil
}

func extractPackage(pkgPath, dest string) error {
	lower := strings.ToLower(pkgPath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return unzipFile(pkgPath, dest)
	default:
		cmd := exec.Command("tar", "-xf", pkgPath, "-C", dest)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("tar extract: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

func unzipFile(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry escapes staging dir: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if err := writeReaderToFile(target, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

func findBundleRoot(staging string) (string, error) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(staging, e.Name()), nil
		}
	}
	if fileExists(filepath.Join(staging, "bin")) {
		return staging, nil
	}
	return "", fmt.Errorf("release bundle root not found under %s", staging)
}

func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeReaderToFile(path string, r io.Reader) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
