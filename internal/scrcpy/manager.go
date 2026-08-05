package scrcpy

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"adbsuite/internal/procutil"
)

type Session struct {
	Serial string `json:"serial"`
	PID    int    `json:"pid"`
	Args   string `json:"args"`
}

type Options struct {
	MaxSize   int
	BitRate   string
	MaxFps    int
	StayAwake bool
	NoAudio   bool
	Title     string
}

type Manager struct {
	mu      sync.Mutex
	bin     func() string
	procs   map[string]*exec.Cmd
	sessions map[string]Session
}

func New(bin func() string) *Manager {
	return &Manager{
		bin:      bin,
		procs:    map[string]*exec.Cmd{},
		sessions: map[string]Session{},
	}
}

func (m *Manager) Start(serial string, opt Options) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.procs[serial]; ok {
		return fmt.Errorf("设备 %s 已在投屏中", serial)
	}
	bin := m.bin()
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		// may be on PATH
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("未找到 scrcpy: %s（请将 scrcpy 放入 bin/scrcpy/ 或在设置中指定路径）", bin)
		}
	}
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	if opt.MaxSize > 0 {
		args = append(args, "-m", fmt.Sprintf("%d", opt.MaxSize))
	}
	if opt.BitRate != "" {
		args = append(args, "-b", opt.BitRate)
	}
	if opt.MaxFps > 0 {
		args = append(args, "--max-fps", fmt.Sprintf("%d", opt.MaxFps))
	}
	if opt.StayAwake {
		args = append(args, "--stay-awake")
	}
	if opt.NoAudio {
		args = append(args, "--no-audio")
	}
	if opt.Title != "" {
		args = append(args, "--window-title", opt.Title)
	}
	cmd := exec.Command(bin, args...)
	// hide console flash; scrcpy still opens its own SDL window
	procutil.HideConsole(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// workdir = scrcpy folder so DLLs resolve
	if dir := filepathDir(bin); dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 scrcpy 失败: %w", err)
	}
	m.procs[serial] = cmd
	m.sessions[serial] = Session{Serial: serial, PID: cmd.Process.Pid, Args: fmt.Sprintf("%v", args)}
	go func(s string, c *exec.Cmd) {
		_ = c.Wait()
		m.mu.Lock()
		delete(m.procs, s)
		delete(m.sessions, s)
		m.mu.Unlock()
	}(serial, cmd)
	return nil
}

func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}

func (m *Manager) Stop(serial string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd, ok := m.procs[serial]
	if !ok {
		return fmt.Errorf("设备 %s 未在投屏", serial)
	}
	_ = killProcess(cmd)
	delete(m.procs, serial)
	delete(m.sessions, serial)
	return nil
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for s, cmd := range m.procs {
		_ = killProcess(cmd)
		delete(m.procs, s)
		delete(m.sessions, s)
	}
}

func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

func (m *Manager) IsRunning(serial string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.procs[serial]
	return ok
}

func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		// taskkill tree for scrcpy children
		k := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T", "/F")
		procutil.HideConsole(k)
		_ = k.Run()
		return nil
	}
	return cmd.Process.Kill()
}
