package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPackageLoadingHelper(t *testing.T) {
	mode := os.Getenv("ADBSUITE_PACKAGES_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "third":
		fmt.Println("package:/data/app/user/base.apk=com.example.user")
	case "system":
		fmt.Println("package:/system/app/Sys.apk=com.example.system")
	case "disabled":
		fmt.Println("package:com.example.user")
	case "uninstalled":
		fmt.Println("package:/data/app/user/base.apk=com.example.user")
		fmt.Println("package:/system/app/Sys.apk=com.example.system")
		fmt.Println("package:/system/app/Old.apk=com.example.old")
	case "versions":
		fmt.Print("Package [com.example.user]\n  versionCode=42\n  versionName=4.2\n")
	case "codes":
		fmt.Println("package:com.example.user versionCode:42")
	case "fail":
		fmt.Fprintln(os.Stderr, "mock disconnected")
		os.Exit(1)
	}
	os.Exit(0)
}

func packageHelper(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPackageLoadingHelper$")
	cmd.Env = append(os.Environ(), "ADBSUITE_PACKAGES_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
	return cmd
}

// A subprocess keeps the package-global cache isolated from all other tests.
func TestListPackageBasicsDoesNotWaitForMetadata(t *testing.T) {
	if os.Getenv("ADBSUITE_BASICS_TEST") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestListPackageBasicsDoesNotWaitForMetadata$")
		cmd.Env = append(os.Environ(), "ADBSUITE_BASICS_TEST=1", "GORACE=atexit_sleep_ms=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return
	}
	labelCacheOnce.Do(func() {
		labelCacheMem = map[string]string{labelCacheKey("com.example.user", "/data/app/user/base.apk"): "Cached user"}
	})
	var calls []string
	client := &Client{command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if !strings.Contains(joined, "shell pm list packages") {
			t.Fatalf("basic list requested slow metadata: %s", joined)
		}
		mode := ""
		switch args[len(args)-1] {
		case "-3":
			mode = "third"
		case "-s":
			mode = "system"
		case "-d":
			mode = "disabled"
		case "-u":
			mode = "uninstalled"
		default:
			t.Fatalf("unexpected list command: %s", joined)
		}
		return packageHelper(ctx, mode)
	}}
	list, err := client.ListPackageBasics("device")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || len(list) != 3 {
		t.Fatalf("calls=%d rows=%d", len(calls), len(list))
	}
	byName := map[string]PackageInfo{}
	for _, p := range list {
		byName[p.Name] = p
	}
	user := byName["com.example.user"]
	if user.Label != "Cached user" || user.LabelPending || !user.Disabled {
		t.Fatal("cached name/state missing")
	}
	if !byName["com.example.system"].LabelPending || !byName["com.example.system"].System {
		t.Fatal("cache miss/system state missing")
	}
	if !byName["com.example.old"].Uninstalled {
		t.Fatal("residual app missing")
	}
	os.Exit(0)
}

func TestPackageVersionsIndependentAndFallback(t *testing.T) {
	for _, mode := range []string{"versions", "codes", "fail"} {
		t.Run(mode, func(t *testing.T) {
			client := &Client{command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "dumpsys") {
					if mode == "versions" {
						return packageHelper(ctx, mode)
					}
					return packageHelper(ctx, "fail")
				}
				if !strings.Contains(joined, "--show-versioncode") {
					t.Fatalf("unexpected command: %s", joined)
				}
				return packageHelper(ctx, mode)
			}}
			input := []PackageInfo{{Name: "com.example.user", Label: "cached", Disabled: true}}
			out, err := client.PackageVersions("device", input)
			if mode == "fail" {
				if err == nil {
					t.Fatal("metadata failure hidden")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if out[0].VersionCode != 42 || out[0].Label != "cached" || !out[0].Disabled {
				t.Fatal("metadata/state mismatch")
			}
			if mode == "versions" && out[0].VersionName != "4.2" {
				t.Fatal("name version missing")
			}
			if input[0].VersionCode != 0 {
				t.Fatal("input slice mutated")
			}
		})
	}
}

func TestConnectedPackageBasicsReadOnly(t *testing.T) {
	serial, adbPath := os.Getenv("ADBSUITE_TEST_SERIAL"), os.Getenv("ADBSUITE_TEST_ADB")
	if serial == "" || adbPath == "" {
		t.Skip("opt-in connected device")
	}
	client := &Client{AdbPath: func() string { return adbPath }}
	start := time.Now()
	list, err := client.ListPackageBasics(serial)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("basic list: %d apps, %s", len(list), time.Since(start).Round(time.Millisecond))
	if len(list) == 0 {
		t.Fatal("no apps")
	}
	start = time.Now()
	versions, err := client.PackageVersions(serial, list)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("background versions: %s", time.Since(start).Round(time.Millisecond))
	if len(versions) != len(list) {
		t.Fatal("metadata changed list size")
	}
}
