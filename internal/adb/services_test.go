package adb

import (
	"strings"
	"testing"
)

const sampleDumpsysServices = `ACTIVITY MANAGER SERVICES (dumpsys activity services)
  User 0 active services:
  * ServiceRecord{789b74b u0 com.google.android.gms/.nearby.presence.service.LifeCycleService c:com.google.android.gms}
    intent={cmp=com.google.android.gms/.nearby.presence.service.LifeCycleService}
    packageName=com.google.android.gms
    processName=com.google.android.gms.persistent
    targetSdkVersion=37
    baseDir=/data/app/~~0axAX4eyLqVmLZsy6T9fiA==/com.google.android.gms-MzzG81T5Itd-ckNGjOG_Sw==/base.apk
    dataDir=/data/user/0/com.google.android.gms
    app=ProcessRecord{2bb1803 5975:com.google.android.gms.persistent/u0a125}
    startForegroundCount=0
    createTime=-5h6m46s523ms startingBgTimeout=--
    lastActivity=-5h6m46s523ms restartTime=-5h6m46s523ms createdFromFg=true
    startRequested=true delayedStop=false stopIfKilled=false callStart=true lastStartId=1

  * ServiceRecord{dcdfd8e u0 com.xiaomi.aicr/.access.AiCrCognitionService c:com.xiaomi.aicr}
    intent={cmp=com.xiaomi.aicr/.access.AiCrCognitionService}
    packageName=com.xiaomi.aicr
    processName=com.xiaomi.aicr:cognitionService
    baseDir=/data/app/~~AxJtNhx-YfJI_ChPRBKvyA==/com.xiaomi.aicr-XulooPdrFJjPG4n1S6Gh3w==/base.apk
    app=ProcessRecord{4b5f5d8 11210:com.xiaomi.aicr:cognitionService/u0a123}
    isForeground=true foregroundId=1 types=0x40000000 foregroundNoti=Notification(channel=aicr)
    createTime=-5h7m10s678ms startingBgTimeout=--
    lastActivity=-4h52m48s352ms restartTime=-5h7m10s588ms createdFromFg=true
    startRequested=true delayedStop=false stopIfKilled=false callStart=true lastStartId=1

  * ServiceRecord{abc123 u0 com.android.systemui/.ImageWallpaper}
    intent={cmp=com.android.systemui/.ImageWallpaper}
    packageName=com.android.systemui
    processName=com.android.systemui
    baseDir=/system_ext/priv-app/SystemUI/SystemUI.apk
    app=ProcessRecord{deadbee 2000:com.android.systemui/u0a67}
    startRequested=false delayedStop=false
    createTime=-1d2h lastActivity=-30s

  Connection bindings to services:
  * ConnectionRecord{xxx}
`

func TestParseRunningServices(t *testing.T) {
	sys := map[string]struct{}{
		"com.android.systemui":    {},
		"com.google.android.gms": {},
	}
	list := parseRunningServices(sampleDumpsysServices, sys)
	if len(list) != 3 {
		t.Fatalf("want 3 services, got %d: %+v", len(list), list)
	}

	// third-party first (aicr), then system packages alphabetically
	// system: gms, systemui — third: aicr
	// sort: !system first → aicr, then gms, systemui
	if list[0].Package != "com.xiaomi.aicr" {
		t.Fatalf("expected third-party first, got %s", list[0].Package)
	}
	if !list[0].Foreground {
		t.Fatal("aicr should be foreground")
	}
	if list[0].PID != 11210 {
		t.Fatalf("aicr pid: got %d", list[0].PID)
	}
	if list[0].Service != "AiCrCognitionService" {
		t.Fatalf("short name: got %s", list[0].Service)
	}
	if list[0].StartRequested != true {
		t.Fatal("startRequested")
	}

	var gms, sysui *RunningService
	for i := range list {
		switch list[i].Package {
		case "com.google.android.gms":
			gms = &list[i]
		case "com.android.systemui":
			sysui = &list[i]
		}
	}
	if gms == nil || sysui == nil {
		t.Fatalf("missing system packages: %+v", list)
	}
	if !gms.System || !sysui.System {
		t.Fatalf("system flags: gms=%v sysui=%v", gms.System, sysui.System)
	}
	if gms.PID != 5975 {
		t.Fatalf("gms pid %d", gms.PID)
	}
	if gms.Process != "com.google.android.gms.persistent" {
		t.Fatalf("gms process %s", gms.Process)
	}
	if gms.Client != "com.google.android.gms" {
		t.Fatalf("gms client %s", gms.Client)
	}
	if gms.CreateTime == "" || !strings.Contains(gms.CreateTime, "5h") {
		t.Fatalf("createTime %q", gms.CreateTime)
	}
	// systemui marked system via path even if not in map — already in map
	if sysui.Service != "ImageWallpaper" {
		t.Fatalf("systemui service %s", sysui.Service)
	}
	if sysui.StartRequested {
		t.Fatal("systemui startRequested should be false")
	}
}

func TestParseRunningServices_systemByPath(t *testing.T) {
	// No sys map; baseDir on /system_ext should mark system.
	sample := `
  * ServiceRecord{abc u0 com.android.systemui/.Foo}
    packageName=com.android.systemui
    processName=com.android.systemui
    baseDir=/system_ext/priv-app/SystemUI/SystemUI.apk
    app=ProcessRecord{x 99:com.android.systemui/1000}
`
	list := parseRunningServices(sample, nil)
	if len(list) != 1 {
		t.Fatalf("got %d", len(list))
	}
	if !list[0].System {
		t.Fatal("expected system via baseDir")
	}
}

func TestShortServiceName(t *testing.T) {
	cases := map[string]string{
		"com.foo/.Bar":           "Bar",
		"com.foo/com.foo.Bar":    "Bar",
		"com.foo/.a.b.C$Inner":   "C$Inner",
		"com.foo/Service":        "Service",
	}
	for in, want := range cases {
		if got := shortServiceName(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestSplitSimpleKV(t *testing.T) {
	m := splitSimpleKV("startRequested=true delayedStop=false stopIfKilled=false callStart=true lastStartId=1")
	if m["startRequested"] != "true" || m["delayedStop"] != "false" || m["lastStartId"] != "1" {
		t.Fatalf("%+v", m)
	}
	m2 := splitSimpleKV("isForeground=true foregroundId=1 types=0x40000000")
	if m2["isForeground"] != "true" || m2["foregroundId"] != "1" {
		t.Fatalf("%+v", m2)
	}
}
