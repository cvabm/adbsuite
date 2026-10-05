package adb

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"adbsuite/internal/paths"
	"adbsuite/internal/procutil"
	"github.com/google/uuid"
)

// RecordSession is a manual screenrecord job (start now, stop later).
type RecordSession struct {
	Serial    string `json:"serial"`
	LocalPath string `json:"localPath"`
	Remote    string `json:"remote"`
	StartedAt int64  `json:"startedAt"`
	Running   bool   `json:"running"`
}

// Recorder implements Android Studio–style recording:
//
//	adb shell screenrecord <remote.mp4>
//
// Stop = Ctrl+C to the host adb process (SIGINT forwarded to screenrecord) so
// the mp4 gets a valid moov atom, then pull to Desktop. No scrcpy window.
type Recorder struct {
	mu       sync.Mutex
	adb      func() string
	jobs     map[string]*recordJob
	starting map[string]bool
}

type recordJob struct {
	cmd       *exec.Cmd
	done      chan struct{}
	stderr    *procutil.Output
	localPath string
	remote    string
	pidFile   string
	remotePID int
	startedAt int64
	waitErr   error
	stopping  bool
}

func NewRecorder(adbPath func() string) *Recorder {
	return &Recorder{
		adb:      adbPath,
		jobs:     map[string]*recordJob{},
		starting: map[string]bool{},
	}
}

const remoteRecordDir = "/sdcard/Movies"

// Start begins device screenrecord. Empty localPath → Desktop timestamped file.
func (r *Recorder) Start(serial, localPath string) (string, error) {
	r.mu.Lock()
	if _, ok := r.jobs[serial]; ok || r.starting[serial] {
		r.mu.Unlock()
		return "", fmt.Errorf("设备 %s 已在录屏中", serial)
	}
	r.starting[serial] = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.starting, serial); r.mu.Unlock() }()

	if localPath == "" {
		localPath = paths.DesktopFile(fmt.Sprintf("adbsuite_%d.mp4", time.Now().UnixNano()))
	}
	if dir := filepath.Dir(localPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	if abs, err := filepath.Abs(localPath); err == nil {
		localPath = abs
	}

	ts := time.Now().Unix()
	id := uuid.NewString()
	remote := fmt.Sprintf("%s/adbsuite_rec_%s.mp4", remoteRecordDir, id)
	pidFile := fmt.Sprintf("%s/adbsuite_rec_%s.pid", remoteRecordDir, id)

	prepCtx, prepCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer prepCancel()
	prep := exec.CommandContext(prepCtx, r.adb(), withSerial(serial, "shell", "mkdir -p "+shellQuote(remoteRecordDir))...)
	procutil.HideConsole(prep)
	if output, err := prep.CombinedOutput(); err != nil {
		return "", fmt.Errorf("准备录屏目录失败: %w；%s", err, strings.TrimSpace(string(output)))
	}

	// Same model as Android Studio / CLI:
	//   adb shell screenrecord /sdcard/Movies/xxx.mp4
	// Write shell pid then exec so kill -INT $pid targets screenrecord after exec.
	// Host adb is started interruptible so Stop can send Ctrl+C (→ remote SIGINT).
	script := fmt.Sprintf("echo $$ > %s; exec screenrecord --time-limit 180 %s", shellQuote(pidFile), shellQuote(remote))
	args := withSerial(serial, "shell", "sh", "-c", shellQuote(script))
	cmd := exec.Command(r.adb(), args...)
	procutil.HideConsoleInterruptible(cmd)
	var stderr procutil.Output
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动录屏失败: %w", err)
	}

	done := make(chan struct{})
	job := &recordJob{
		cmd:       cmd,
		done:      done,
		stderr:    &stderr,
		localPath: localPath,
		remote:    remote,
		pidFile:   pidFile,
		startedAt: ts,
	}

	r.mu.Lock()
	if _, ok := r.jobs[serial]; ok {
		r.mu.Unlock()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("设备 %s 已在录屏中", serial)
	}
	r.jobs[serial] = job
	r.mu.Unlock()

	go func() {
		job.waitErr = cmd.Wait()
		close(done)
	}()

	// Resolve remote PID in background — do NOT block the UI on adb round-trips.
	// (A sync poll loop was making "Stop" stay disabled for several seconds.)
	go func() {
		for i := 0; i < 40; i++ {
			select {
			case <-done:
				return
			case <-time.After(50 * time.Millisecond):
			}
			if p, err := readRemotePID(r.adb, serial, pidFile); err == nil && p > 0 {
				r.mu.Lock()
				if j, ok := r.jobs[serial]; ok && j == job {
					j.remotePID = p
				}
				r.mu.Unlock()
				return
			}
		}
	}()

	// Only wait briefly for instant failure (bad path / unsupported). Then return
	// so the frontend can enable「停止录屏」immediately — same as Android Studio.
	select {
	case <-done:
		msg := strings.TrimSpace(stderr.String())
		r.mu.Lock()
		if r.jobs[serial] == job {
			delete(r.jobs, serial)
		}
		r.mu.Unlock()
		if msg == "" {
			msg = "screenrecord 立即退出（设备可能不支持或无存储权限）"
		}
		return "", fmt.Errorf("启动录屏失败: %s", msg)
	case <-time.After(300 * time.Millisecond):
		return localPath, nil
	}
}

// Stop sends Ctrl+C to host adb (and SIGINT on-device), waits for finalize, pulls to Desktop.
func (r *Recorder) Stop(serial string, client *Client) (string, error) {
	r.mu.Lock()
	job, ok := r.jobs[serial]
	if !ok {
		r.mu.Unlock()
		return "", fmt.Errorf("设备 %s 未在录屏", serial)
	}
	if job.stopping {
		r.mu.Unlock()
		return "", fmt.Errorf("设备 %s 正在停止录屏", serial)
	}
	job.stopping = true
	remotePID := job.remotePID
	r.mu.Unlock()
	finished := false
	defer func() {
		r.mu.Lock()
		if r.jobs[serial] == job {
			if finished {
				delete(r.jobs, serial)
			} else {
				job.stopping = false
			}
		}
		r.mu.Unlock()
	}()

	// No artificial minimum duration — Android Studio also stops immediately.
	// We only wait for screenrecord to actually exit after SIGINT (usually <1s).

	alreadyDone := waitJobDone(job, 0)

	if !alreadyDone {
		// 1) Device-side SIGINT (primary on many Windows setups where console
		//    events do not reach adb). screenrecord writes moov on SIGINT.
		signalScreenrecord(client, serial, job.pidFile, remotePID, job.remote)

		// 2) Host-side Ctrl+C / CTRL_BREAK — same as terminal stop in Studio/CLI.
		if job.cmd != nil && job.cmd.Process != nil {
			_ = procutil.InterruptPID(job.cmd.Process.Pid)
		}

		// Wait until adb shell exits (finalize finished). Fast path: often 100–800ms.
		if !waitJobDone(job, 5*time.Second) {
			signalScreenrecord(client, serial, job.pidFile, remotePID, job.remote)
			if job.cmd != nil && job.cmd.Process != nil {
				_ = procutil.InterruptPID(job.cmd.Process.Pid)
			}
			_ = waitRemoteProcessGone(client, serial, remotePID, job.pidFile, job.remote, 3*time.Second)
			if !waitJobDone(job, 3*time.Second) {
				// Last resort only — may yield incomplete file; still try pull+validate.
				if job.cmd != nil && job.cmd.Process != nil {
					_ = job.cmd.Process.Kill()
				}
				_ = waitJobDone(job, 1*time.Second)
			}
		}
	}
	if !waitJobDone(job, time.Second) || !waitRemoteProcessGone(client, serial, remotePID, job.pidFile, job.remote, 3*time.Second) {
		return "", fmt.Errorf("无法确认本次录屏已停止，设备文件保留在 %s；未停止其他录屏", job.remote)
	}
	finished = true

	// Brief readiness poll (exits as soon as size is stable; no fixed 2s sleep).
	_ = waitRemoteFileReady(client, serial, job.remote, 3*time.Second)

	remote := job.remote

	local := job.localPath
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(150 * time.Millisecond)
		}
		if _, err := client.Pull(serial, remote, local); err != nil {
			lastErr = err
			if strings.Contains(strings.ToLower(err.Error()), "no such file") {
				detail := strings.TrimSpace(job.stderr.String())
				if detail != "" {
					return "", fmt.Errorf("设备上没有录屏文件 %s；%s", remote, detail)
				}
				return "", fmt.Errorf("设备上没有录屏文件 %s", remote)
			}
			continue
		}
		if err := validateMP4File(local); err != nil {
			lastErr = err
			continue
		}
		_, _ = client.Shell(serial, fmt.Sprintf("rm -f %s %s", shellQuote(remote), shellQuote(job.pidFile)))
		return local, nil
	}

	if sz, err := remoteFileSize(client, serial, remote); err == nil && sz > 0 {
		return local, fmt.Errorf("录屏未完整封装: %v；设备文件仍在 %s，可手动 pull 试播", lastErr, remote)
	}
	if lastErr != nil {
		return "", fmt.Errorf("停止录屏失败: %v", lastErr)
	}
	return "", fmt.Errorf("停止录屏失败：未得到可播放文件")
}

// Inspect both executable and exact output argument before signalling an owned PID.
// Never enumerate/kill all screenrecord processes, including as a fallback.
func recordingProcessCommand(pidFile string, pid int, remote string, signal bool) string {
	prefix := fmt.Sprintf("p=%d\n", pid)
	if pidFile != "" {
		prefix += "if [ \"$p\" -le 0 ]; then p=$(cat " + shellQuote(pidFile) + " 2>/dev/null); fi\n"
	}
	prefix += `case "$p" in ''|0|*[!0-9]*) echo UNKNOWN; exit 1;; esac
if [ ! -d "/proc/$p" ]; then echo GONE; exit 0; fi
if [ ! -r "/proc/$p/cmdline" ]; then echo UNKNOWN; exit 1; fi
args=$(tr '\000' '\n' < "/proc/$p/cmdline")
exe=$(printf '%s\n' "$args" | head -n 1)
case "${exe##*/}" in screenrecord) ;; *) echo GONE; exit 0;; esac
`
	prefix += "if ! printf '%s\\n' \"$args\" | grep -F -x -- " + shellQuote(remote) + "; then echo GONE; exit 0; fi\n"
	if signal {
		return prefix + "kill -2 \"$p\"\n"
	}
	return prefix + "echo ALIVE\n"
}

func signalScreenrecord(client *Client, serial, pidFile string, pid int, remote string) {
	_, _ = client.Shell(serial, recordingProcessCommand(pidFile, pid, remote, true))
}

func waitRemoteProcessGone(client *Client, serial string, pid int, pidFile, remote string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := client.RunTimeout(serial, 2*time.Second, "shell", recordingProcessCommand(pidFile, pid, remote, false))
		if err == nil && strings.HasSuffix(strings.TrimSpace(res.Stdout), "GONE") {
			return true
		}
		time.Sleep(80 * time.Millisecond)
	}
	return false
}

func waitJobDone(job *recordJob, timeout time.Duration) bool {
	if job == nil || job.done == nil {
		return true
	}
	if timeout <= 0 {
		select {
		case <-job.done:
			return true
		default:
			return false
		}
	}
	select {
	case <-job.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func waitRemoteFileReady(client *Client, serial, remote string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last int64 = -1
	stable := 0
	for time.Now().Before(deadline) {
		sz, err := remoteFileSize(client, serial, remote)
		if err == nil && sz > 0 {
			if sz == last {
				stable++
				// two identical samples ~80ms apart is enough after process exit
				if stable >= 2 {
					return nil
				}
			} else {
				stable = 0
				last = sz
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	if last > 0 {
		return nil
	}
	return fmt.Errorf("远程录屏文件尚未就绪: %s", remote)
}

func remoteFileSize(client *Client, serial, remote string) (int64, error) {
	for _, cmd := range []string{
		"stat -c %s " + shellQuote(remote),
		"wc -c < " + shellQuote(remote),
		"ls -l " + shellQuote(remote),
	} {
		res, err := client.Shell(serial, cmd)
		if err != nil {
			continue
		}
		out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined))
		if out == "" {
			continue
		}
		fields := strings.Fields(out)
		if strings.Contains(cmd, "ls -l") {
			if len(fields) >= 5 {
				if n2, e2 := strconv.ParseInt(fields[4], 10, 64); e2 == nil {
					return n2, nil
				}
			}
			continue
		}
		for _, f := range fields {
			if n, e := strconv.ParseInt(f, 10, 64); e == nil && n >= 0 {
				return n, nil
			}
		}
	}
	return 0, fmt.Errorf("无法读取远程文件大小")
}

func readRemotePID(adbPath func() string, serial, pidFile string) (int, error) {
	args := withSerial(serial, "shell", "cat", pidFile)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, adbPath(), args...)
	procutil.HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	s = strings.TrimSuffix(s, "\r")
	if s == "" {
		return 0, fmt.Errorf("empty pid")
	}
	if i := strings.IndexAny(s, " \t\n\r"); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("bad pid %q", s)
	}
	return n, nil
}

func validateMP4File(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Size() < 32 {
		return fmt.Errorf("文件过小（%d 字节），录制可能未产生有效数据", st.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	hasFtyp, hasMoov, walkErr := walkMP4Atoms(f, st.Size())
	if walkErr != nil {
		return walkErr
	}
	if !hasMoov {
		hasMoov = tailContainsAtom(f, st.Size(), "moov")
	}
	if !hasFtyp {
		hasFtyp = headContainsAtom(f, "ftyp")
	}
	if !hasFtyp {
		return fmt.Errorf("不是有效的 MP4（缺少 ftyp）")
	}
	if !hasMoov {
		return fmt.Errorf("MP4 未完整封装（缺少 moov）")
	}
	return nil
}

func walkMP4Atoms(f *os.File, fileSize int64) (hasFtyp, hasMoov bool, err error) {
	var off int64
	for off+8 <= fileSize {
		var hdr [8]byte
		if _, err = f.ReadAt(hdr[:], off); err != nil {
			return hasFtyp, hasMoov, err
		}
		size32 := binary.BigEndian.Uint32(hdr[0:4])
		typ := string(hdr[4:8])
		var boxSize int64
		headerLen := int64(8)
		switch size32 {
		case 0:
			boxSize = fileSize - off
		case 1:
			var ext [8]byte
			if _, err = f.ReadAt(ext[:], off+8); err != nil {
				return hasFtyp, hasMoov, err
			}
			boxSize = int64(binary.BigEndian.Uint64(ext[:]))
			headerLen = 16
		default:
			boxSize = int64(size32)
		}
		if boxSize < headerLen {
			return hasFtyp, hasMoov, nil
		}
		switch typ {
		case "ftyp":
			hasFtyp = true
		case "moov":
			hasMoov = true
		}
		if hasFtyp && hasMoov {
			return hasFtyp, hasMoov, nil
		}
		if boxSize <= 0 {
			break
		}
		next := off + boxSize
		if next <= off {
			break
		}
		off = next
	}
	return hasFtyp, hasMoov, nil
}

func headContainsAtom(f *os.File, atom string) bool {
	buf := make([]byte, 64)
	n, _ := f.ReadAt(buf, 0)
	return n >= 8 && strings.Contains(string(buf[:n]), atom)
}

func tailContainsAtom(f *os.File, fileSize int64, atom string) bool {
	const maxTail = 512 * 1024
	n := fileSize
	if n > maxTail {
		n = maxTail
	}
	if n < 8 {
		return false
	}
	buf := make([]byte, int(n))
	if _, err := f.ReadAt(buf, fileSize-n); err != nil && err != io.EOF {
		return false
	}
	name := []byte(atom)
	for i := 0; i+8 <= len(buf); i++ {
		if buf[i+4] == name[0] && buf[i+5] == name[1] && buf[i+6] == name[2] && buf[i+7] == name[3] {
			return true
		}
	}
	return false
}

func (r *Recorder) IsRecording(serial string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.jobs[serial]
	return ok
}

func (r *Recorder) List() []RecordSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordSession, 0, len(r.jobs))
	for serial, job := range r.jobs {
		running := true
		select {
		case <-job.done:
			running = false
		default:
		}
		out = append(out, RecordSession{
			Serial:    serial,
			LocalPath: job.localPath,
			Remote:    job.remote,
			StartedAt: job.startedAt,
			Running:   running,
		})
	}
	return out
}

func (r *Recorder) StopAll(client *Client) {
	r.mu.Lock()
	serials := make([]string, 0, len(r.jobs))
	for s := range r.jobs {
		serials = append(serials, s)
	}
	r.mu.Unlock()
	for _, s := range serials {
		_, _ = r.Stop(s, client)
	}
}

func withSerial(serial string, args ...string) []string {
	if serial == "" {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "-s", serial)
	out = append(out, args...)
	return out
}
