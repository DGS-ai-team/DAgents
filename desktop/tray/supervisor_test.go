//go:build windows

package main

import (
	"testing"
	"time"
)

func TestSupervisor_showRunning_debounce(t *testing.T) {
	s := &supervisor{}
	if s.showRunning() {
		t.Fatal("expected not running before any probe")
	}

	s.recordProbeFail()
	if s.showRunning() {
		t.Fatal("single failure should not flip UI to stopped when starting from unknown")
	}

	s.recordProbeOK()
	if !s.showRunning() {
		t.Fatal("expected running after probe OK")
	}

	s.recordProbeFail()
	if !s.showRunning() {
		t.Fatal("single failure should not hide running UI")
	}
	s.recordProbeFail()
	if s.showRunning() {
		t.Fatal("expected stopped after consecutive failures")
	}

	s.recordProbeOK()
	if !s.showRunning() {
		t.Fatal("expected running restored after probe OK")
	}
}

func TestSupervisor_shouldRecover_thresholdAndBackoff(t *testing.T) {
	s := &supervisor{}
	now := time.Unix(0, 0)

	if s.shouldRecover(now, false, false) {
		t.Fatal("should not recover before probe failure threshold")
	}

	s.recordProbeFail()
	s.recordProbeFail()
	if !s.shouldRecover(now, false, false) {
		t.Fatal("expected recover after threshold failures")
	}
	if s.shouldRecover(now, true, false) {
		t.Fatal("holdStopped should block recover")
	}

	s.markRecoverAttempt(now)
	s.recordRecoverFail()
	if s.shouldRecover(now.Add(9*time.Second), false, false) {
		t.Fatal("expected 10s backoff after first recover failure")
	}
	if !s.shouldRecover(now.Add(10*time.Second), false, false) {
		t.Fatal("expected recover allowed after 10s backoff")
	}

	s.markRecoverAttempt(now.Add(10 * time.Second))
	s.recordRecoverFail()
	if !s.shouldRecover(now.Add(40*time.Second), false, false) {
		t.Fatal("expected recover allowed after 30s second-step backoff")
	}
	if s.shouldRecover(now.Add(39*time.Second), false, false) {
		t.Fatal("expected 30s backoff after second recover failure")
	}

	s.recordProbeOK()
	if s.shouldRecover(now.Add(time.Hour), false, false) {
		t.Fatal("healthy probe should reset recover trigger")
	}
}
