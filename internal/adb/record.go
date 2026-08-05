package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"adbsuite/internal/procutil"
)

// RecordSession is a manual screenrecord job (start now, stop later).
type RecordSession struct {
	Serial    string `json:"serial"`
	LocalPath string `json:"localPath"`
	Remote    string `json:"remote"`
	StartedAt int64  `json:"startedAt"`
	Running   bool   `json:"running"`
}

type Recorder struct {
	mu    sync.Mutex
	adb   func() string
	jobs  map[string]*recordJob
}

type recordJob struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	localPath string
	remote    string
	startedAt int64
}

func NewRecorder(adbPath func() string) *Recorder {
	return &Recorder{
		adb:  adbPath,
		jobs: map[string]*recordJob{},
	}
}

// Start begins screenrecord without relying on a short timer.
// Android still caps screenrecord (~3 min on many devices); stop early via Stop.
func (r *Recorder) Start(serial, localPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[serial]; ok {
		return fmt.Errorf("设备 %s 已在录屏中", serial)
	}
	if localPath == "" {
		localPath = filepath.Join(os.TempDir(), fmt.Sprintf("adbsuite_%d.mp4", time.Now().Unix()))
	}
	if dir := filepath.Dir(localPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	remote := fmt.Sprintf("/sdcard/adbsuite_rec_%d.mp4", time.Now().Unix())

	// best-effort clean + start; --time-limit 180 is Android max on many builds,
	// user can stop earlier; we do not block the UI on duration.
	pre := exec.Command(r.adb(), withSerial(serial, "shell", "rm", "-f", remote)...)
	procutil.HideConsole(pre)
	_ = pre.Run()

	ctx, cancel := context.WithCancel(context.Background())
	// time-limit 180 = platform max; manual stop kills before that
	args := withSerial(serial, "shell", "screenrecord", "--time-limit", "180", remote)
	cmd := exec.CommandContext(ctx, r.adb(), args...)
	procutil.HideConsole(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("启动录屏失败: %w", err)
	}
	job := &recordJob{
		cmd:       cmd,
		cancel:    cancel,
		localPath: localPath,
		remote:    remote,
		startedAt: time.Now().Unix(),
	}
	r.jobs[serial] = job
	go func() {
		_ = cmd.Wait()
		// process ended by itself (limit or device); leave job so Stop can still pull,
		// mark via clearing cmd wait already done — Stop handles pull.
	}()
	return nil
}

// Stop interrupts recording, pulls the file, and cleans remote.
func (r *Recorder) Stop(serial string, client *Client) (string, error) {
	r.mu.Lock()
	job, ok := r.jobs[serial]
	if !ok {
		r.mu.Unlock()
		return "", fmt.Errorf("设备 %s 未在录屏", serial)
	}
	delete(r.jobs, serial)
	r.mu.Unlock()

	// Prefer SIGINT on device so screenrecord finalizes a valid mp4
	_, _ = client.Shell(serial, "pkill -2 screenrecord 2>/dev/null; killall -2 screenrecord 2>/dev/null; true")
	time.Sleep(300 * time.Millisecond)
	if job.cancel != nil {
		job.cancel()
	}
	if job.cmd != nil && job.cmd.Process != nil {
		_ = job.cmd.Process.Kill()
	}
	// give device a moment to flush file
	time.Sleep(500 * time.Millisecond)

	local := job.localPath
	if _, err := client.Pull(serial, job.remote, local); err != nil {
		// try once more after short wait
		time.Sleep(500 * time.Millisecond)
		if _, err2 := client.Pull(serial, job.remote, local); err2 != nil {
			_, _ = client.Shell(serial, "rm -f "+job.remote)
			return "", fmt.Errorf("拉取录屏失败: %w", err)
		}
	}
	_, _ = client.Shell(serial, "rm -f "+job.remote)
	return local, nil
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
		running := job.cmd != nil && job.cmd.ProcessState == nil
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
