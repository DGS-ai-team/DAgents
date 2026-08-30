//go:build windows

package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallReleasePackage_zip(t *testing.T) {
	home := t.TempDir()
	pkgPath := filepath.Join(t.TempDir(), "bundle.zip")
	if err := writeTestReleaseZip(pkgPath); err != nil {
		t.Fatal(err)
	}
	transaction, err := installReleasePackage(home, pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	transaction.Commit()
	got, err := os.ReadFile(filepath.Join(home, "bin", "dagents-node.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "node-binary" {
		t.Fatalf("node exe = %q", got)
	}
	ver, err := os.ReadFile(filepath.Join(home, "VERSION"))
	if err != nil || string(ver) != "0.9.0\n" {
		t.Fatalf("VERSION = %q err=%v", ver, err)
	}
}

func writeTestReleaseZip(path string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	w := zip.NewWriter(out)
	files := map[string]string{
		"bundle-0.9.0/bin/dagents-node.exe": "node-binary",
		"bundle-0.9.0/VERSION":              "0.9.0\n",
	}
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			out.Close()
			return err
		}
		if _, err := f.Write([]byte(content)); err != nil {
			out.Close()
			return err
		}
	}
	if err := w.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
