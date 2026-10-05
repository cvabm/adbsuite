package adb

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordingSignalsOnlyOwnedPID(t *testing.T) {
	remote := "/sdcard/Movies/adbsuite_rec_unique.mp4"
	command := recordingProcessCommand("/sdcard/Movies/unique.pid", 123, remote, true)
	for _, unsafe := range []string{"killall", "pkill", "pidof", "ps -A"} {
		if strings.Contains(command, unsafe) {
			t.Fatalf("broad process lookup: %s", unsafe)
		}
	}
	if !strings.Contains(command, "case \"${exe##*/}\" in screenrecord)") || !strings.Contains(command, "grep -F -x -- "+shellQuote(remote)) {
		t.Fatal("missing executable or exact output-path ownership check")
	}
	if !strings.HasSuffix(command, "kill -2 \"$p\"\n") {
		t.Fatal("missing targeted signal")
	}
	if strings.Contains(recordingProcessCommand("file.pid", 0, remote, false), "kill ") {
		t.Fatal("read-only process inspection sends a signal")
	}
}

func TestRecordingCommandQuotesPathsAndRejectsInvalidPID(t *testing.T) {
	command := recordingProcessCommand("a'b.pid", -1, "a'b.mp4", true)
	if !strings.Contains(command, shellQuote("a'b.pid")) || !strings.Contains(command, shellQuote("a'b.mp4")) {
		t.Fatal("paths not escaped")
	}
	if !strings.Contains(command, "''|0|*[!0-9]*) echo UNKNOWN; exit 1") {
		t.Fatal("missing PID validation")
	}
}

func writeBox(b []byte, size uint32, typ string, payload []byte) []byte {
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], size)
	copy(hdr[4:8], typ)
	b = append(b, hdr[:]...)
	b = append(b, payload...)
	return b
}

func TestValidateMP4File_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.mp4")
	// ftyp (8+8) + free padding + moov
	var data []byte
	ftypPayload := []byte("isom\x00\x00\x02\x00isomiso2")
	data = writeBox(data, uint32(8+len(ftypPayload)), "ftyp", ftypPayload)
	pad := make([]byte, 2000)
	data = writeBox(data, uint32(8+len(pad)), "free", pad)
	moovPayload := make([]byte, 64)
	data = writeBox(data, uint32(8+len(moovPayload)), "moov", moovPayload)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateMP4File(path); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidateMP4File_MissingMoov(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mp4")
	var data []byte
	ftypPayload := []byte("isom\x00\x00\x02\x00isomiso2")
	data = writeBox(data, uint32(8+len(ftypPayload)), "ftyp", ftypPayload)
	pad := make([]byte, 2000)
	data = writeBox(data, uint32(8+len(pad)), "mdat", pad)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateMP4File(path); err == nil {
		t.Fatal("expected missing moov error")
	}
}

func TestValidateMP4File_TooSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.mp4")
	if err := os.WriteFile(path, []byte("notmp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateMP4File(path); err == nil {
		t.Fatal("expected too small")
	}
}
