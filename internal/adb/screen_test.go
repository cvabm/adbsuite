package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPortHelper(t *testing.T) {
	mode := os.Getenv("ADBSUITE_PORT_HELPER")
	if mode == "" {
		return
	}
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "mock permission denied")
		os.Exit(1)
	}
	fmt.Println("mock mapping")
	os.Exit(0)
}

func TestPortErrorsAreReported(t *testing.T) {
	for _, method := range []string{"remove", "list"} {
		for _, failures := range []string{"", "forward", "reverse", "forward reverse"} {
			t.Run(method+"/"+failures, func(t *testing.T) {
				var calls []string
				client := &Client{command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
					direction := args[2] // -s device forward|reverse ...
					calls = append(calls, direction)
					mode := "ok"
					if strings.Contains(failures, direction) {
						mode = "fail"
					}
					cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPortHelper$")
					cmd.Env = append(os.Environ(), "ADBSUITE_PORT_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
					return cmd
				}}
				var err error
				if method == "remove" {
					err = client.PortRemoveAll("device")
				} else {
					_, err = client.PortList("device")
				}
				if len(calls) != 2 || calls[0] != "forward" || calls[1] != "reverse" {
					t.Fatal("did not try both directions")
				}
				if failures == "" && err != nil {
					t.Fatal(err)
				}
				if failures != "" {
					if err == nil {
						t.Fatal("failure swallowed")
					}
					for _, direction := range strings.Fields(failures) {
						if !strings.Contains(err.Error(), direction) {
							t.Fatalf("missing %s error", direction)
						}
					}
				}
			})
		}
	}
}
