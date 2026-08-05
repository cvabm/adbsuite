package adb

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"adbsuite/internal/procutil"

	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
)

// PackageInfo is one installed app.
// Name is the package id (for adb ops); Label is the human display name.
type PackageInfo struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"`
	System bool   `json:"system"` // true = system app, false = user/third-party
}

func (c *Client) Install(serial, apkPath string, reinstall, downgrade, grantAll bool) (string, error) {
	args := []string{"install"}
	if reinstall {
		args = append(args, "-r")
	}
	if downgrade {
		args = append(args, "-d")
	}
	if grantAll {
		args = append(args, "-g")
	}
	args = append(args, apkPath)
	res, err := c.RunTimeout(serial, 10*time.Minute, args...)
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Combined), nil
}

// Uninstall removes an app. For system apps uses `pm uninstall --user 0` (current user).
func (c *Client) Uninstall(serial, pkg string, keepData, isSystem bool) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", fmt.Errorf("包名为空")
	}
	if isSystem {
		// System apps usually cannot be fully removed without root; uninstall for current user.
		cmd := "pm uninstall"
		if keepData {
			cmd += " -k"
		}
		cmd += " --user 0 " + shellQuote(pkg)
		res, err := c.Shell(serial, cmd)
		if err != nil {
			return res.Combined, err
		}
		return strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined)), nil
	}
	args := []string{"uninstall"}
	if keepData {
		args = append(args, "-k")
	}
	args = append(args, pkg)
	res, err := c.RunTimeout(serial, 2*time.Minute, args...)
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Combined), nil
}

// ListPackages lists installed apps.
// filter: "third" (普通/第三方), "system" (系统), "all" (全部). Empty defaults to "third".
func (c *Client) ListPackages(serial string, filter string) ([]PackageInfo, error) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	switch filter {
	case "", "user", "3", "third-party", "thirdparty":
		filter = "third"
	case "sys", "s":
		filter = "system"
	case "all", "*":
		filter = "all"
	}

	var list []PackageInfo
	var err error
	switch filter {
	case "third":
		list, err = c.listPackagesFlag(serial, "-3", false)
	case "system":
		list, err = c.listPackagesFlag(serial, "-s", true)
	default:
		third, err1 := c.listPackagesFlag(serial, "-3", false)
		sys, err2 := c.listPackagesFlag(serial, "-s", true)
		if err1 != nil {
			return nil, err1
		}
		if err2 != nil {
			return nil, err2
		}
		byName := make(map[string]PackageInfo, len(third)+len(sys))
		for _, p := range third {
			byName[p.Name] = p
		}
		for _, p := range sys {
			byName[p.Name] = p
		}
		list = make([]PackageInfo, 0, len(byName))
		for _, p := range byName {
			list = append(list, p)
		}
	}
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []PackageInfo{}
	}

	// Fill human labels (cached + concurrent resolve).
	c.fillLabels(serial, list)

	// 普通应用在前，系统应用在后；同组内按应用名排序
	sort.Slice(list, func(i, j int) bool {
		if list[i].System != list[j].System {
			return !list[i].System && list[j].System
		}
		li, lj := list[i].Label, list[j].Label
		if li == lj {
			return list[i].Name < list[j].Name
		}
		return strings.ToLower(li) < strings.ToLower(lj)
	})
	return list, nil
}

// listPackagesFlag runs `pm list packages -f [flag]` (-3 third-party, -s system).
func (c *Client) listPackagesFlag(serial, flag string, system bool) ([]PackageInfo, error) {
	args := []string{"shell", "pm", "list", "packages", "-f"}
	if flag != "" {
		args = append(args, flag)
	}
	res, err := c.RunTimeout(serial, 60*time.Second, args...)
	if err != nil {
		return nil, err
	}
	var list []PackageInfo
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		// package:/data/app/xxx.apk=com.example
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		body := strings.TrimPrefix(line, "package:")
		// path may contain '=' (e.g. /data/app/~~xx==/pkg-yy==/base.apk); split on last '='
		eq := strings.LastIndex(body, "=")
		if eq <= 0 || eq == len(body)-1 {
			list = append(list, PackageInfo{
				Name:   body,
				Label:  friendlyFallback(body),
				System: system,
			})
			continue
		}
		path, name := body[:eq], body[eq+1:]
		list = append(list, PackageInfo{
			Name:   name,
			Path:   path,
			Label:  friendlyFallback(name),
			System: system,
		})
	}
	if list == nil {
		list = []PackageInfo{}
	}
	return list, nil
}

func (c *Client) ClearPackage(serial, pkg string) (string, error) {
	res, err := c.Shell(serial, "pm clear "+shellQuote(pkg))
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (c *Client) PackagePath(serial, pkg string) (string, error) {
	res, err := c.Shell(serial, "pm path "+shellQuote(pkg))
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(res.Stdout)
	if line == "" {
		return "", fmt.Errorf("未找到包: %s", pkg)
	}
	for _, l := range strings.Split(line, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "package:") {
			return strings.TrimPrefix(l, "package:"), nil
		}
	}
	return line, nil
}

func (c *Client) PidOf(serial, pkg string) (string, error) {
	res, err := c.Shell(serial, "pidof "+shellQuote(pkg))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ---------- label resolution ----------

type labelCacheFile struct {
	// key: name + "\x00" + path → label
	Entries map[string]string `json:"entries"`
}

var (
	labelCacheMu   sync.Mutex
	labelCacheMem  map[string]string
	labelCachePath string
	labelCacheOnce sync.Once
)

func labelCacheKey(name, path string) string {
	return name + "\x00" + path
}

func initLabelCache() {
	labelCacheOnce.Do(func() {
		labelCacheMem = map[string]string{}
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = "."
		}
		dir = filepath.Join(dir, "adbsuite")
		_ = os.MkdirAll(dir, 0o755)
		labelCachePath = filepath.Join(dir, "app_labels.json")
		data, err := os.ReadFile(labelCachePath)
		if err != nil {
			return
		}
		var f labelCacheFile
		if json.Unmarshal(data, &f) == nil && f.Entries != nil {
			labelCacheMem = f.Entries
		}
	})
}

func getCachedLabel(name, path string) (string, bool) {
	initLabelCache()
	labelCacheMu.Lock()
	defer labelCacheMu.Unlock()
	v, ok := labelCacheMem[labelCacheKey(name, path)]
	return v, ok && v != ""
}

func putCachedLabels(entries map[string]string) {
	if len(entries) == 0 {
		return
	}
	initLabelCache()
	labelCacheMu.Lock()
	defer labelCacheMu.Unlock()
	for k, v := range entries {
		if k != "" && v != "" {
			labelCacheMem[k] = v
		}
	}
	f := labelCacheFile{Entries: labelCacheMem}
	data, err := json.Marshal(f)
	if err == nil && labelCachePath != "" {
		_ = os.WriteFile(labelCachePath, data, 0o644)
	}
}

func (c *Client) fillLabels(serial string, list []PackageInfo) {
	type job struct {
		idx  int
		name string
		path string
	}
	var jobs []job
	for i := range list {
		if list[i].Path == "" {
			continue
		}
		if lab, ok := getCachedLabel(list[i].Name, list[i].Path); ok {
			list[i].Label = lab
			continue
		}
		jobs = append(jobs, job{idx: i, name: list[i].Name, path: list[i].Path})
	}
	if len(jobs) == 0 {
		return
	}

	// Keep concurrency modest: many large arsc buffers + flaky parsers.
	const workers = 6
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	newEntries := make(map[string]string, len(jobs))
	for _, j := range jobs {
		j := j
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// Extra safety: never let a single bad APK kill the process.
			defer func() {
				if recover() != nil {
					mu.Lock()
					fb := friendlyFallback(j.name)
					list[j.idx].Label = fb
					newEntries[labelCacheKey(j.name, j.path)] = fb
					mu.Unlock()
				}
			}()
			label := c.resolveAppLabel(serial, j.path)
			if label == "" {
				label = friendlyFallback(j.name)
			}
			mu.Lock()
			list[j.idx].Label = label
			newEntries[labelCacheKey(j.name, j.path)] = label
			mu.Unlock()
		}()
	}
	wg.Wait()
	putCachedLabels(newEntries)
}

func (c *Client) resolveAppLabel(serial, apkPath string) (label string) {
	// androidbinary panics on some OEM/corrupt resources.arsc — must not crash UI.
	defer func() {
		if recover() != nil {
			label = ""
		}
	}()

	apkPath = strings.TrimSpace(apkPath)
	if apkPath == "" {
		return ""
	}
	// Prefer base.apk if path points at a split directory entry without filename.
	if strings.HasSuffix(apkPath, "/") {
		apkPath = apkPath + "base.apk"
	}

	man, err := c.execOutBytes(serial, 30*time.Second, "unzip", "-p", apkPath, "AndroidManifest.xml")
	if err != nil || len(man) < 8 {
		return ""
	}
	arsc, err := c.execOutBytes(serial, 45*time.Second, "unzip", "-p", apkPath, "resources.arsc")
	if err != nil || !isLikelyResTable(arsc) {
		return ""
	}

	tmp, err := os.CreateTemp("", "adbsuite-mini-*.apk")
	if err != nil {
		return ""
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	zw := zip.NewWriter(tmp)
	for _, it := range []struct {
		name string
		data []byte
	}{
		{"AndroidManifest.xml", man},
		{"resources.arsc", arsc},
	} {
		w, err := zw.Create(it.name)
		if err != nil {
			_ = zw.Close()
			_ = tmp.Close()
			return ""
		}
		if _, err := io.Copy(w, bytes.NewReader(it.data)); err != nil {
			_ = zw.Close()
			_ = tmp.Close()
			return ""
		}
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		return ""
	}
	if err := tmp.Close(); err != nil {
		return ""
	}

	ap, err := apk.OpenFile(tmpPath)
	if err != nil {
		return ""
	}
	defer ap.Close()

	// Prefer Chinese labels for this audience.
	cfgs := []*androidbinary.ResTableConfig{
		{Language: [2]uint8{'z', 'h'}, Country: [2]uint8{'C', 'N'}},
		{Language: [2]uint8{'z', 'h'}, Country: [2]uint8{'T', 'W'}},
		{Language: [2]uint8{'z', 'h'}, Country: [2]uint8{'H', 'K'}},
		{Language: [2]uint8{'z', 'h'}},
		nil,
	}
	var best string
	for _, cfg := range cfgs {
		lab, err := ap.Label(cfg)
		if err != nil || strings.TrimSpace(lab) == "" {
			continue
		}
		lab = strings.TrimSpace(lab)
		if containsCJK(lab) {
			return lab
		}
		if best == "" {
			best = lab
		}
	}
	return best
}

// isLikelyResTable rejects empty/corrupt arsc before androidbinary panics on them.
func isLikelyResTable(b []byte) bool {
	// ResChunk_header: type(u16)=0x0002 RES_TABLE_TYPE, headerSize(u16), size(u32)
	if len(b) < 12 {
		return false
	}
	typ := uint16(b[0]) | uint16(b[1])<<8
	if typ != 0x0002 {
		return false
	}
	size := uint32(b[4]) | uint32(b[5])<<8 | uint32(b[6])<<16 | uint32(b[7])<<24
	// size should be plausible; adb noise can produce truncated blobs
	if size < 12 || int(size) > len(b)+1024 {
		// allow slight mismatch but require minimum real content
		if len(b) < 64 {
			return false
		}
	}
	return true
}

// execOutBytes runs adb exec-out and returns raw bytes (binary-safe).
func (c *Client) execOutBytes(serial string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmdArgs := make([]string, 0, len(args)+3)
	if serial != "" {
		cmdArgs = append(cmdArgs, "-s", serial)
	}
	cmdArgs = append(cmdArgs, "exec-out")
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, c.path(), cmdArgs...)
	procutil.HideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

func friendlyFallback(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return pkg
	}
	// last segment of package name, e.g. com.tencent.mm → mm
	if i := strings.LastIndex(pkg, "."); i >= 0 && i+1 < len(pkg) {
		return pkg[i+1:]
	}
	return pkg
}

func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han) {
			return true
		}
	}
	return false
}
