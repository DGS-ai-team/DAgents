package processlock

import "errors"

// ErrAlreadyRunning 表示本配置下已有 Node 实例在运行。
var ErrAlreadyRunning = errors.New("dagents-node already running for this config")

// Release 释放 Acquire 得到的锁。
type Release func()
