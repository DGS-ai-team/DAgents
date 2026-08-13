package tools

import "os/exec"

// processTreeHandle owns the platform-specific container used to terminate a
// shell together with all descendants. A nil handle means the platform
// fallback remains in use.
type processTreeHandle interface {
	terminate()
	close()
}

func terminateProcessTree(cmd *exec.Cmd, tree processTreeHandle) {
	if tree != nil {
		tree.terminate()
		return
	}
	killShellProcess(cmd)
}

func closeProcessTree(tree processTreeHandle) {
	if tree != nil {
		tree.close()
	}
}
