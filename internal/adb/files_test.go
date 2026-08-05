package adb

import (
	"testing"
)

func TestParseLsLaISO(t *testing.T) {
	out := `total 12
drwxrwx---  5 system sdcard_rw 3452 2024-01-02 12:00 .
drwxr-xr-x 10 root   root      4096 2024-01-01 00:00 ..
drwxr-xr-x  2 u0_a1  u0_a1     4096 2024-03-15 08:30 Download
-rw-r--r--  1 u0_a1  u0_a1      123 2024-03-15 09:00 hello world.txt
lrwxrwxrwx  1 root   root        11 2024-01-01 00:00 sdcard -> /storage/self/primary
`
	entries, err := parseLsLa("/sdcard", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries: %+v", len(entries), entries)
	}
	// dirs first
	if !entries[0].IsDir || entries[0].Name != "Download" {
		t.Fatalf("expected Download dir first, got %+v", entries[0])
	}
	var file *RemoteEntry
	for i := range entries {
		if entries[i].Name == "hello world.txt" {
			file = &entries[i]
		}
	}
	if file == nil {
		t.Fatal("missing spaced filename")
	}
	if file.Size != 123 || file.IsDir {
		t.Fatalf("bad file: %+v", file)
	}
	var link *RemoteEntry
	for i := range entries {
		if entries[i].Name == "sdcard" {
			link = &entries[i]
		}
	}
	if link == nil || !link.IsLink || link.Link != "/storage/self/primary" {
		t.Fatalf("bad link: %+v", link)
	}
}

func TestParseLsLaMonth(t *testing.T) {
	out := `total 8
-rw-r--r-- 1 root root 42 Jan  5 12:00 a.txt
drwxr-xr-x 2 root root 4096 Feb  1  2023 bdir
`
	entries, err := parseLsLa("/tmp", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d", len(entries))
	}
	if !entries[0].IsDir || entries[0].Name != "bdir" {
		t.Fatalf("dir first: %+v", entries[0])
	}
}

func TestNormalizeRemotePath(t *testing.T) {
	if normalizeRemotePath("") != "/sdcard" {
		t.Fatal("default")
	}
	if normalizeRemotePath("/sdcard/") != "/sdcard" {
		t.Fatal("trim slash")
	}
	if normalizeRemotePath("a/b") != "/a/b" {
		t.Fatal("abs")
	}
}

func TestJoinParent(t *testing.T) {
	if joinRemote("/sdcard", "Download") != "/sdcard/Download" {
		t.Fatal(joinRemote("/sdcard", "Download"))
	}
	if parentRemote("/sdcard/Download") != "/sdcard" {
		t.Fatal(parentRemote("/sdcard/Download"))
	}
	if parentRemote("/") != "/" {
		t.Fatal("root parent")
	}
}
