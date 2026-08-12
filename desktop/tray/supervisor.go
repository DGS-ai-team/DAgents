package main

import "time"

const (
	// 连续探活失败次数达到阈值后才触发自动重启。
	recoverProbeFailThreshold = 2
	// 连续探活失败后，托盘才从「运行中」切到「未运行」（防抖）。
	statusDownThreshold = 2
	// 连续探活成功后，托盘从「未运行」切到「运行中」。
	statusUpThreshold = 1
)

var recoverBackoffSteps = []time.Duration{
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

// supervisor 管理 Node 监护：探活防抖、失败阈值与重启退避。
type supervisor struct {
	probeFailStreak int
	probeOKStreak   int

	uiRunning     bool
	uiInitialized bool

	recoverFailStreak  int
	lastRecoverAttempt time.Time
}

func (s *supervisor) recordProbeOK() {
	s.probeOKStreak++
	s.probeFailStreak = 0
	s.recoverFailStreak = 0
}

func (s *supervisor) recordProbeFail() {
	s.probeFailStreak++
	s.probeOKStreak = 0
}

func (s *supervisor) showRunning() bool {
	if !s.uiInitialized {
		s.uiInitialized = true
		s.uiRunning = s.probeOKStreak >= statusUpThreshold
		return s.uiRunning
	}
	if s.uiRunning {
		if s.probeFailStreak >= statusDownThreshold {
			s.uiRunning = false
		}
	} else if s.probeOKStreak >= statusUpThreshold {
		s.uiRunning = true
	}
	return s.uiRunning
}

func (s *supervisor) shouldRecover(now time.Time, holdStopped, recovering bool) bool {
	if holdStopped || recovering {
		return false
	}
	if s.probeFailStreak < recoverProbeFailThreshold {
		return false
	}
	if s.lastRecoverAttempt.IsZero() {
		return true
	}
	return now.Sub(s.lastRecoverAttempt) >= s.recoverBackoff()
}

func (s *supervisor) recoverBackoff() time.Duration {
	if s.recoverFailStreak <= 0 {
		return recoverBackoffSteps[0]
	}
	idx := s.recoverFailStreak - 1
	if idx >= len(recoverBackoffSteps) {
		return recoverBackoffSteps[len(recoverBackoffSteps)-1]
	}
	return recoverBackoffSteps[idx]
}

func (s *supervisor) markRecoverAttempt(now time.Time) {
	s.lastRecoverAttempt = now
}

func (s *supervisor) recordRecoverFail() {
	s.recoverFailStreak++
}

func (s *supervisor) recordRecoverOK() {
	s.recoverFailStreak = 0
	s.lastRecoverAttempt = time.Time{}
}

func (s *supervisor) resetAfterManualStart() {
	s.probeFailStreak = 0
	s.probeOKStreak = statusUpThreshold
	s.recoverFailStreak = 0
	s.lastRecoverAttempt = time.Time{}
	s.uiRunning = true
	s.uiInitialized = true
}

func (s *supervisor) resetAfterManualStop() {
	s.probeFailStreak = 0
	s.probeOKStreak = 0
	s.recoverFailStreak = 0
	s.lastRecoverAttempt = time.Time{}
	s.uiRunning = false
	s.uiInitialized = true
}
