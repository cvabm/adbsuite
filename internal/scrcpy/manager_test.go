package scrcpy

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestScrcpyHelper(t *testing.T) {
	mode := os.Getenv("ADBSUITE_SCRCPY_HELPER")
	if mode == "" {
		return
	}
	if mode == "late" {
		time.Sleep(700 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "mock scrcpy: invalid option")
	os.Exit(17)
}

func fakeManager(mode string) *Manager {
	m := New(func() string { return os.Args[0] })
	m.command = func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestScrcpyHelper$")
		cmd.Env = append(os.Environ(), "ADBSUITE_SCRCPY_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
		return cmd
	}
	return m
}

func TestOldScrcpyExitCannotRemoveReplacement(t *testing.T) {
	m := New(nil)
	old, current := &exec.Cmd{}, &exec.Cmd{}
	m.procs["device"] = current
	m.sessions["device"] = Session{Serial: "device", PID: 2}
	called := false
	m.OnExit(func(Session, error) { called = true })
	m.finishProcess("device", old, fmt.Errorf("old failure"))
	if !m.IsRunning("device") || m.List()[0].PID != 2 || called {
		t.Fatal("old exit affected the replacement")
	}
	m.finishProcess("device", current, nil)
	if m.IsRunning("device") || !called {
		t.Fatal("current exit was not handled")
	}
}

func TestScrcpyStartupFailureIncludesOutput(t *testing.T) {
	m := fakeManager("early")
	err := m.Start("device", Options{})
	if err == nil || !strings.Contains(err.Error(), "invalid option") {
		t.Fatalf("missing process error: %v", err)
	}
	if m.IsRunning("device") {
		t.Fatal("failed process remains active")
	}
}

func TestLaterScrcpyFailureIsReported(t *testing.T) {
	m := fakeManager("late")
	reported := make(chan error, 1)
	m.OnExit(func(s Session, err error) {
		if s.Serial != "device" {
			t.Error("wrong device")
		}
		reported <- err
	})
	if err := m.Start("device", Options{}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		if err == nil || !strings.Contains(err.Error(), "invalid option") {
			t.Fatalf("missing late error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exit notification timed out")
	}
}
