package logcat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLogcatHelper(t *testing.T) {
	mode := os.Getenv("ADBSUITE_LOGCAT_HELPER")
	if mode == "" {
		return
	}
	if mode == "clear-fail" {
		fmt.Fprintln(os.Stderr, "permission denied")
		os.Exit(1)
	}
	fmt.Println("mock log line")
	if mode == "hold" {
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

func fakeStreamer(mode string) *Streamer {
	s := New()
	s.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLogcatHelper$")
		cmd.Env = append(os.Environ(), "ADBSUITE_LOGCAT_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
		return cmd
	}
	return s
}

func TestOldLogcatExitCannotClearReplacement(t *testing.T) {
	s := New()
	old, current := &exec.Cmd{}, &exec.Cmd{}
	s.cmd, s.serial = current, "new-device"
	if s.finishProcess(old) || !s.Running() || s.Serial() != "new-device" {
		t.Fatal("old exit cleared new stream")
	}
	if !s.finishProcess(current) || s.Running() {
		t.Fatal("current exit not cleared")
	}
}

func TestLogcatClearFailureIsNotHidden(t *testing.T) {
	s := fakeStreamer("clear-fail")
	if err := s.Start("mock", "device", true, nil, nil); err == nil {
		t.Fatal("clear failure was ignored")
	}
	if s.Running() {
		t.Fatal("stream started after clear failure")
	}
}

func TestLogcatRapidStopRestart(t *testing.T) {
	s := fakeStreamer("hold")
	defer s.Stop()
	for i := 0; i < 8; i++ {
		lines := make(chan string, 1)
		if err := s.Start("mock", "device", false, func(line string) { lines <- line }, nil); err != nil {
			t.Fatal(err)
		}
		select {
		case <-lines:
		case <-time.After(5 * time.Second):
			t.Fatal("replacement stopped producing logs")
		}
		if !s.Running() {
			t.Fatal("stream state cleared by obsolete cleanup")
		}
		s.Stop()
	}
}

func TestNaturalLogcatExitCleansBeforeNotification(t *testing.T) {
	s := fakeStreamer("exit")
	stopped := make(chan struct{}, 1)
	if err := s.Start("mock", "device", false, nil, func(reason, _ string) {
		if s.Running() {
			t.Error("finished stream remains active")
		}
		if reason != "disconnected" {
			t.Errorf("unexpected reason: %s", reason)
		}
		stopped <- struct{}{}
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("missing stop notification")
	}
}
