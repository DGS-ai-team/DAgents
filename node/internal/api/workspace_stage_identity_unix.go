//go:build !windows

package api

import (
	"fmt"
	"os"
	"syscall"
)

func openWorkspaceStageForRead(root, id string) (*os.File, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.OpenFile(id, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}

func openWorkspaceTargetForRead(root *os.Root, rel string) (*os.File, error) {
	return root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}

func workspaceFileIdentity(f *os.File) (string, error) {
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	raw, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("file identity unavailable")
	}
	return fmt.Sprintf("unix:%x:%x", uint64(raw.Dev), uint64(raw.Ino)), nil
}

func workspacePathIsReparse(_ string, info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }

func workspacePathComponentInvalid(string) bool { return false }
