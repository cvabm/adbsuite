package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"adbsuite/internal/adb"
	"adbsuite/internal/logcat"
	"adbsuite/internal/paths"
	"adbsuite/internal/procutil"
	"adbsuite/internal/scrcpy"
	"adbsuite/internal/settings"
	"adbsuite/internal/tasks"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx      context.Context
	store    *settings.Store
	client   *adb.Client
	scrcpy   *scrcpy.Manager
	logcat   *logcat.Streamer
	queue    *tasks.Queue
	recorder *adb.Recorder
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	store, err := settings.NewStore()
	if err != nil {
		store, _ = settings.NewStore()
	}
	a.store = store
	a.client = &adb.Client{AdbPath: a.adbPath}
	a.scrcpy = scrcpy.New(a.scrcpyPath)
	a.logcat = logcat.New()
	a.recorder = adb.NewRecorder(a.adbPath)
	cfg := a.store.Get()
	a.queue = tasks.New(cfg.MaxConcurrency)
	a.queue.OnChange(func() {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "tasks:update", a.queue.List())
		}
	})
	// 预拉起 adb server，避免首次 devices 时额外闪进程
	go func() {
		cmd := exec.Command(a.adbPath(), "start-server")
		procutil.HideConsole(cmd)
		_ = cmd.Run()
	}()
}

// Bootstrap 一次返回启动所需数据，减少前端多次往返/重绘
type BootstrapData struct {
	Settings settings.Settings   `json:"settings"`
	Paths    map[string]string   `json:"paths"`
	Devices  []adb.Device        `json:"devices"`
	Scrcpy   []scrcpy.Session    `json:"scrcpy"`
	Tasks    []tasks.Item        `json:"tasks"`
	AdbOK    bool                `json:"adbOk"`
	AdbMsg   string              `json:"adbMsg"`
}

func (a *App) Bootstrap() BootstrapData {
	cfg := a.store.Get()
	out := BootstrapData{
		Settings: cfg,
		Paths: map[string]string{
			"adb":    a.adbPath(),
			"scrcpy": a.scrcpyPath(),
		},
		Devices: []adb.Device{},
		Scrcpy:  []scrcpy.Session{},
		Tasks:   []tasks.Item{},
	}
	if devs, err := a.client.ListDevices(); err == nil {
		out.Devices = devs
	}
	out.Scrcpy = a.scrcpy.List()
	out.Tasks = a.queue.List()
	if v, err := a.client.Version(); err != nil {
		out.AdbOK = false
		out.AdbMsg = err.Error()
	} else {
		out.AdbOK = true
		// 只回一行，避免刷输出
		out.AdbMsg = firstLine(v)
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func (a *App) shutdown(ctx context.Context) {
	a.logcat.Stop()
	if a.recorder != nil {
		a.recorder.StopAll(a.client)
	}
	if a.store != nil && a.store.Get().KillScrcpyOnExit {
		a.scrcpy.StopAll()
	}
}

func (a *App) adbPath() string {
	cfg := a.store.Get()
	return paths.AdbPath(cfg.AdbPath)
}

func (a *App) scrcpyPath() string {
	cfg := a.store.Get()
	return paths.ScrcpyPath(cfg.ScrcpyPath)
}

// ---------- settings ----------

func (a *App) GetSettings() settings.Settings {
	return a.store.Get()
}

func (a *App) SaveSettings(s settings.Settings) error {
	if err := a.store.Save(s); err != nil {
		return err
	}
	a.queue.SetMax(s.MaxConcurrency)
	return nil
}

func (a *App) GetToolPaths() map[string]string {
	return map[string]string{
		"adb":    a.adbPath(),
		"scrcpy": a.scrcpyPath(),
	}
}

func (a *App) TestAdb() (string, error) {
	return a.client.Version()
}

// ---------- devices ----------

func (a *App) ListDevices() ([]adb.Device, error) {
	return a.client.ListDevices()
}

func (a *App) DeviceInfo(serial string) (adb.DeviceInfo, error) {
	return a.client.DeviceInfo(serial)
}

func (a *App) Reboot(serial, mode string) error {
	return a.client.Reboot(serial, mode)
}

func (a *App) ForegroundActivity(serial string) (string, error) {
	return a.client.ForegroundActivity(serial)
}

// ---------- wireless ----------

func (a *App) Tcpip(serial string, port int) (string, error) {
	if port <= 0 {
		port = a.store.Get().TcpipPort
	}
	return a.client.Tcpip(serial, port)
}

func (a *App) Connect(host string) (string, error) {
	out, err := a.client.Connect(host)
	if err == nil {
		a.store.AddRecentHost(host)
	}
	return out, err
}

func (a *App) Disconnect(target string) (string, error) {
	return a.client.Disconnect(target)
}

func (a *App) Pair(hostPort, code string) (string, error) {
	return a.client.Pair(hostPort, code)
}

// ---------- apps ----------

func (a *App) InstallApk(serial, apk string, reinstall, downgrade, grantAll bool) (string, error) {
	return a.client.Install(serial, apk, reinstall, downgrade, grantAll)
}

func (a *App) UninstallPackage(serial, pkg string, keepData, isSystem bool) (string, error) {
	return a.client.Uninstall(serial, pkg, keepData, isSystem)
}

// ListPackages filter: "third" | "system" | "disabled" | "uninstalled" | "all"
func (a *App) ListPackages(serial string, filter string) ([]adb.PackageInfo, error) {
	return a.client.ListPackages(serial, filter)
}

func (a *App) ClearPackage(serial, pkg string) (string, error) {
	return a.client.ClearPackage(serial, pkg)
}

func (a *App) LaunchPackage(serial, pkg string) (string, error) {
	return a.client.LaunchPackage(serial, pkg)
}

func (a *App) ForceStopPackage(serial, pkg string) (string, error) {
	return a.client.ForceStopPackage(serial, pkg)
}

func (a *App) DisablePackage(serial, pkg string) (string, error) {
	return a.client.DisablePackage(serial, pkg)
}

func (a *App) EnablePackage(serial, pkg string) (string, error) {
	return a.client.EnablePackage(serial, pkg)
}

// ---------- files (Device Explorer) ----------

func (a *App) PushFile(serial, local, remote string) (string, error) {
	return a.client.Push(serial, local, remote)
}

func (a *App) PullFile(serial, remote, local string) (string, error) {
	return a.client.Pull(serial, remote, local)
}

// ListRemoteDir returns a text listing (legacy). Prefer ListRemoteEntries.
func (a *App) ListRemoteDir(serial, remote string) (string, error) {
	return a.client.ListDir(serial, remote)
}

// ListRemoteEntries returns structured directory entries for Device Explorer.
func (a *App) ListRemoteEntries(serial, remote string) ([]adb.RemoteEntry, error) {
	return a.client.ListDirEntries(serial, remote)
}

func (a *App) MkdirRemote(serial, remote string) error {
	return a.client.Mkdir(serial, remote)
}

func (a *App) DeleteRemote(serial, remote string, recursive bool) error {
	return a.client.DeleteRemote(serial, remote, recursive)
}

func (a *App) RenameRemote(serial, oldPath, newPath string) error {
	return a.client.RenameRemote(serial, oldPath, newPath)
}

// ---------- screen / ports ----------

func (a *App) Screenshot(serial, localPath string) (string, error) {
	return a.client.Screenshot(serial, localPath)
}

func (a *App) StartScreenRecord(serial, localPath string) error {
	return a.recorder.Start(serial, localPath)
}

func (a *App) StopScreenRecord(serial string) (string, error) {
	return a.recorder.Stop(serial, a.client)
}

func (a *App) IsScreenRecording(serial string) bool {
	return a.recorder.IsRecording(serial)
}

func (a *App) ListScreenRecords() []adb.RecordSession {
	return a.recorder.List()
}

func (a *App) Shell(serial, command string) (string, error) {
	res, err := a.client.Shell(serial, command)
	if err != nil {
		return res.Combined, err
	}
	return res.Stdout, nil
}

func (a *App) Forward(serial, local, remote string) (string, error) {
	return a.client.Forward(serial, local, remote)
}

func (a *App) Reverse(serial, remote, local string) (string, error) {
	return a.client.Reverse(serial, remote, local)
}

func (a *App) PortList(serial string) (map[string]string, error) {
	fw, _ := a.client.ForwardList(serial)
	rv, _ := a.client.ReverseList(serial)
	return map[string]string{"forward": fw, "reverse": rv}, nil
}

func (a *App) PortRemoveAll(serial string) error {
	_ = a.client.ForwardRemoveAll(serial)
	_ = a.client.ReverseRemoveAll(serial)
	return nil
}

// ---------- scrcpy ----------

func (a *App) StartScrcpy(serial string) error {
	cfg := a.store.Get()
	opt := scrcpy.Options{
		MaxSize:   cfg.ScrcpyMaxSize,
		BitRate:   cfg.ScrcpyBitRate,
		MaxFps:    cfg.ScrcpyMaxFps,
		StayAwake: cfg.ScrcpyStayAwake,
		NoAudio:   cfg.ScrcpyNoAudio,
		Title:     "scrcpy " + serial,
	}
	return a.scrcpy.Start(serial, opt)
}

func (a *App) StopScrcpy(serial string) error {
	return a.scrcpy.Stop(serial)
}

func (a *App) ListScrcpy() []scrcpy.Session {
	return a.scrcpy.List()
}

func (a *App) IsScrcpyRunning(serial string) bool {
	return a.scrcpy.IsRunning(serial)
}

// ---------- logcat ----------

func (a *App) StartLogcat(serial string, clearFirst bool) error {
	return a.logcat.Start(a.adbPath(), serial, clearFirst, func(line string) {
		runtime.EventsEmit(a.ctx, "logcat:line", line)
	}, func(reason, message string) {
		runtime.EventsEmit(a.ctx, "logcat:stopped", map[string]string{
			"reason":  reason,
			"message": message,
			"serial":  serial,
		})
	})
}

func (a *App) StopLogcat() {
	a.logcat.Stop()
}

func (a *App) IsLogcatRunning() bool {
	return a.logcat.Running()
}

// ---------- tasks (multi-device) ----------

func (a *App) ListTasks() []tasks.Item {
	return a.queue.List()
}

func (a *App) ClearFinishedTasks() {
	a.queue.ClearFinished()
}

func (a *App) CancelTask(id string) {
	a.queue.Cancel(id)
}

func (a *App) BatchInstall(serials []string, apk string, reinstall, downgrade, grantAll bool) []string {
	label := "安装 " + filepath.Base(apk)
	return a.queue.EnqueueMany("install", label, serials, func(serial string) (string, error) {
		return a.client.Install(serial, apk, reinstall, downgrade, grantAll)
	})
}

func (a *App) BatchScreenshot(serials []string, dir string) []string {
	if dir == "" {
		dir = "."
	}
	return a.queue.EnqueueMany("screenshot", "截图", serials, func(serial string) (string, error) {
		safe := strings.ReplaceAll(serial, ":", "_")
		path := filepath.Join(dir, fmt.Sprintf("%s_%d.png", safe, time.Now().Unix()))
		return a.client.Screenshot(serial, path)
	})
}

func (a *App) BatchStartScrcpy(serials []string) []string {
	return a.queue.EnqueueMany("scrcpy", "投屏", serials, func(serial string) (string, error) {
		if a.scrcpy.IsRunning(serial) {
			return "已在投屏", nil
		}
		err := a.StartScrcpy(serial)
		if err != nil {
			return "", err
		}
		return "已启动", nil
	})
}

func (a *App) BatchStopScrcpy(serials []string) []string {
	return a.queue.EnqueueMany("scrcpy-stop", "停止投屏", serials, func(serial string) (string, error) {
		if !a.scrcpy.IsRunning(serial) {
			return "未在投屏", nil
		}
		return "已停止", a.scrcpy.Stop(serial)
	})
}

// ---------- dialogs ----------

func (a *App) SelectFile(title string, filters []string) (string, error) {
	opts := runtime.OpenDialogOptions{Title: title}
	if len(filters) > 0 {
		opts.Filters = []runtime.FileFilter{{DisplayName: "Files", Pattern: strings.Join(filters, ";")}}
	}
	return runtime.OpenFileDialog(a.ctx, opts)
}

func (a *App) SelectSaveFile(title, defaultFilename string) (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
	})
}

func (a *App) SelectDirectory(title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}
