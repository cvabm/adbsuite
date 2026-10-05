package adb

import (
	"adbsuite/internal/logcat"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in: only reads device metadata, directory listings, port lists and logs.
func TestConnectedDeviceReadOnly(t *testing.T) {
	serial, adbPath := os.Getenv("ADBSUITE_TEST_SERIAL"), os.Getenv("ADBSUITE_TEST_ADB")
	if serial == "" || adbPath == "" {
		t.Skip("set ADBSUITE_TEST_SERIAL and ADBSUITE_TEST_ADB for read-only device checks")
	}
	client := &Client{AdbPath: func() string { return adbPath }}
	devices, err := client.ListDevices()
	if err != nil {
		t.Fatal("device listing failed")
	}
	found := false
	for _, device := range devices {
		if device.Serial == serial && device.State == "device" {
			found = true
		}
	}
	if !found {
		t.Fatal("specified device is not ready")
	}
	if _, err := client.DeviceInfo(serial); err != nil {
		t.Fatal("device metadata failed")
	}
	if _, err := client.ListDirEntries(serial, "/sdcard"); err != nil {
		t.Fatal("directory listing failed")
	}
	if _, err := client.PortList(serial); err != nil {
		t.Fatal("port listing failed")
	}
	command := recordingProcessCommand("", 1, "/__adbsuite_nonmatching_recording__", false)
	if _, err := client.Shell(serial, "sh -n -c "+shellQuote(command)); err != nil {
		t.Fatal("recording shell syntax invalid")
	}
	command = strings.Replace(command, "p=1\n", "p=$$\n", 1)
	result, err := client.Shell(serial, command)
	if err != nil || !strings.HasSuffix(strings.TrimSpace(result.Stdout), "GONE") {
		t.Fatal("non-recording process was not rejected")
	}
	stream := logcat.New()
	defer stream.Stop()
	lines := make(chan struct{}, 1)
	if err := stream.Start(adbPath, serial, false, func(string) {
		select {
		case lines <- struct{}{}:
		default:
		}
	}, nil); err != nil {
		t.Fatal("read-only logcat failed")
	}
	select {
	case <-lines:
	case <-time.After(10 * time.Second):
		t.Fatal("no logcat sample")
	}
}
