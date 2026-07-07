package singleinstance

import "errors"

// ErrAlreadyRunning 表示同名互斥体已被占用（已有实例在运行）。
var ErrAlreadyRunning = errors.New("another instance is already running")

// Release 释放 Acquire 得到的锁。
type Release func()
