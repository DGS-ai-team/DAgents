//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "dagents-tray 仅支持 Windows（需 CGO + systray）。")
	fmt.Fprintln(os.Stderr, "在 Windows 上构建：")
	fmt.Fprintln(os.Stderr, `  cd desktop/tray && set CGO_ENABLED=1 && go build -o dagents-tray.exe .`)
	os.Exit(1)
}
