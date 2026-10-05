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
	"regexp"
	"sort"
	"strconv"
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
	Name         string `json:"name"`
	Label        string `json:"label"`
	Path         string `json:"path,omitempty"`
	System       bool   `json:"system"` // true = system app, false = user/third-party
	VersionName  string `json:"versionName,omitempty"`
	VersionCode  int64  `json:"versionCode,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`     // pm disabled
	Uninstalled  bool   `json:"uninstalled,omitempty"`  // residual after uninstall (--user / -u)
	LabelPending bool   `json:"labelPending,omitempty"` // cache miss; resolve after displaying the list
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

// ListPackages lists apps with optional filter.
// filter: "third" | "system" | "disabled" | "uninstalled" | "all". Empty defaults to "third".
// Frontend usually requests "all" and filters client-side.
func (c *Client) ListPackages(serial string, filter string) ([]PackageInfo, error) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	switch filter {
	case "", "user", "3", "third-party", "thirdparty":
		filter = "third"
	case "sys", "s":
		filter = "system"
	case "d", "disable", "disabled":
		filter = "disabled"
	case "u", "uninstall", "uninstalled", "removed":
		filter = "uninstalled"
	case "all", "*":
		filter = "all"
	}

	list, err := c.listAllPackages(serial)
	if err != nil {
		return nil, err
	}

	// Fill human labels (cached + concurrent resolve).
	c.fillLabels(serial, list)
	// Fill versionName / versionCode from dumpsys (one bulk call).
	c.fillVersions(serial, list)

	sortPackages(list)

	if filter == "all" {
		return list, nil
	}
	out := make([]PackageInfo, 0, len(list))
	for _, p := range list {
		switch filter {
		case "third":
			if !p.System && !p.Disabled && !p.Uninstalled {
				out = append(out, p)
			}
		case "system":
			if p.System && !p.Disabled && !p.Uninstalled {
				out = append(out, p)
			}
		case "disabled":
			if p.Disabled && !p.Uninstalled {
				out = append(out, p)
			}
		case "uninstalled":
			if p.Uninstalled {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// ListPackageBasics does not read APK resources or dump version information.
// Cached names are immediately usable; cache misses retain a package-name fallback.
func (c *Client) ListPackageBasics(serial string) ([]PackageInfo, error) {
	list, err := c.listAllPackages(serial)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Path == "" {
			continue
		}
		if label, ok := getCachedLabel(list[i].Name, list[i].Path); ok {
			list[i].Label = label
		} else {
			list[i].LabelPending = true
		}
	}
	sortPackages(list)
	return list, nil
}

// PackageLabels resolves one small batch, without re-fetching the device list.
func (c *Client) PackageLabels(serial string, list []PackageInfo) []PackageInfo {
	list = append([]PackageInfo{}, list...)
	c.fillLabels(serial, list)
	for i := range list {
		list[i].LabelPending = false
	}
	return list
}

// PackageVersions runs independently of the slower uncached name resolution.
func (c *Client) PackageVersions(serial string, list []PackageInfo) ([]PackageInfo, error) {
	list = append([]PackageInfo{}, list...)
	if len(list) == 0 {
		return list, nil
	}
	versions := c.fetchVersionsDumpsys(serial)
	if len(versions) == 0 {
		versions = c.fetchVersionCodesOnly(serial)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("无法读取应用版本信息，可重新刷新；应用列表仍可使用")
	}
	applyPackageVersions(list, versions)
	return list, nil
}

func sortPackages(list []PackageInfo) {
	// active third → active system → disabled → uninstalled; name within group.
	sort.Slice(list, func(i, j int) bool {
		ki, kj := packageSortKey(list[i]), packageSortKey(list[j])
		if ki != kj {
			return ki < kj
		}
		li, lj := list[i].Label, list[j].Label
		if li == lj {
			return list[i].Name < list[j].Name
		}
		return strings.ToLower(li) < strings.ToLower(lj)
	})
}

func packageSortKey(p PackageInfo) int {
	if p.Uninstalled {
		return 3
	}
	if p.Disabled {
		return 2
	}
	if p.System {
		return 1
	}
	return 0
}

// listAllPackages builds third + system + disabled flags + residual uninstalled packages.
func (c *Client) listAllPackages(serial string) ([]PackageInfo, error) {
	third, err1 := c.listPackagesFlag(serial, []string{"-3"}, false)
	sys, err2 := c.listPackagesFlag(serial, []string{"-s"}, true)
	if err1 != nil {
		return nil, err1
	}
	if err2 != nil {
		return nil, err2
	}

	byName := make(map[string]PackageInfo, len(third)+len(sys)+32)
	for _, p := range third {
		byName[p.Name] = p
	}
	for _, p := range sys {
		byName[p.Name] = p
	}

	// Disabled packages (still installed).
	if disabled, err := c.listPackageNames(serial, []string{"-d"}); err == nil {
		for name := range disabled {
			if p, ok := byName[name]; ok {
				p.Disabled = true
				byName[name] = p
			} else {
				// Rare: disabled but not in -3/-s snapshot; still surface it.
				byName[name] = PackageInfo{
					Name:     name,
					Label:    friendlyFallback(name),
					Disabled: true,
					System:   false,
				}
			}
		}
	}

	// Residual uninstalled: present in `pm list packages -u` but not currently installed.
	installed := make(map[string]struct{}, len(byName))
	for name := range byName {
		installed[name] = struct{}{}
	}
	if withU, err := c.listPackagesFlag(serial, []string{"-u"}, false); err == nil {
		for _, p := range withU {
			if _, ok := installed[p.Name]; ok {
				continue
			}
			p.Uninstalled = true
			if p.System || isSystemAPKPath(p.Path) {
				p.System = true
			}
			// Prefer path/system hint; default third-party residual.
			byName[p.Name] = p
		}
	}

	list := make([]PackageInfo, 0, len(byName))
	for _, p := range byName {
		list = append(list, p)
	}
	return list, nil
}

// listPackagesFlag runs `pm list packages -f [flags...]`.
// system sets the System field when true; when false, System may still be inferred from path for -u.
func (c *Client) listPackagesFlag(serial string, flags []string, system bool) ([]PackageInfo, error) {
	args := []string{"shell", "pm", "list", "packages", "-f"}
	args = append(args, flags...)
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
			name := body
			list = append(list, PackageInfo{
				Name:   name,
				Label:  friendlyFallback(name),
				System: system || isSystemAPKPath(""),
			})
			continue
		}
		path, name := body[:eq], body[eq+1:]
		sys := system || isSystemAPKPath(path)
		list = append(list, PackageInfo{
			Name:   name,
			Path:   path,
			Label:  friendlyFallback(name),
			System: sys,
		})
	}
	if list == nil {
		list = []PackageInfo{}
	}
	return list, nil
}

// listPackageNames returns package names from `pm list packages [flags...]` (no -f).
func (c *Client) listPackageNames(serial string, flags []string) (map[string]struct{}, error) {
	args := []string{"shell", "pm", "list", "packages"}
	args = append(args, flags...)
	res, err := c.RunTimeout(serial, 60*time.Second, args...)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{})
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "package:"))
		// tolerate "package:name versionCode:x"
		if i := strings.IndexAny(name, " \t"); i > 0 {
			name = name[:i]
		}
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out, nil
}

func isSystemAPKPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	// Common system partitions / locations.
	prefixes := []string{
		"/system/", "/system_ext/", "/product/", "/vendor/",
		"/oem/", "/odm/", "/apex/", "/priv-app/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	// e.g. /system/app/Foo/Foo.apk without trailing slash check above for "/system" alone
	if path == "/system" || strings.HasPrefix(path, "/system/") {
		return true
	}
	return false
}

func (c *Client) ClearPackage(serial, pkg string) (string, error) {
	res, err := c.Shell(serial, "pm clear "+shellQuote(pkg))
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// LaunchPackage starts the app's launcher activity (monkey LAUNCHER intent).
func (c *Client) LaunchPackage(serial, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", fmt.Errorf("包名为空")
	}
	// Prefer monkey: works without resolving activity component manually.
	cmd := "monkey -p " + shellQuote(pkg) + " -c android.intent.category.LAUNCHER 1"
	res, err := c.Shell(serial, cmd)
	out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined))
	if err != nil {
		// Fallback: resolve main activity then am start.
		if out2, err2 := c.launchViaResolve(serial, pkg); err2 == nil {
			return out2, nil
		}
		return out, err
	}
	// monkey prints events injected; treat "No activities found" as failure.
	low := strings.ToLower(out)
	if strings.Contains(low, "no activities") ||
		(strings.Contains(low, "error") && strings.Contains(low, "monkey")) {
		if out2, err2 := c.launchViaResolve(serial, pkg); err2 == nil {
			return out2, nil
		}
		return out, fmt.Errorf("%s", firstNonEmpty(out, "无法启动应用"))
	}
	return out, nil
}

func (c *Client) launchViaResolve(serial, pkg string) (string, error) {
	// cmd package resolve-activity --brief -c android.intent.category.LAUNCHER <pkg>
	res, err := c.Shell(serial, "cmd package resolve-activity --brief -c android.intent.category.LAUNCHER "+shellQuote(pkg))
	if err != nil && strings.TrimSpace(res.Stdout) == "" {
		return "", err
	}
	comp := ""
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "priority=") || strings.HasPrefix(line, "No activity") {
			continue
		}
		// last non-meta line is usually package/activity
		if strings.Contains(line, "/") {
			comp = line
		}
	}
	if comp == "" {
		return "", fmt.Errorf("未找到可启动的 Activity")
	}
	res2, err2 := c.Shell(serial, "am start -n "+shellQuote(comp))
	out := strings.TrimSpace(firstNonEmpty(res2.Stdout, res2.Combined))
	if err2 != nil {
		return out, err2
	}
	return out, nil
}

// ForceStopPackage force-stops a running app.
func (c *Client) ForceStopPackage(serial, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", fmt.Errorf("包名为空")
	}
	res, err := c.Shell(serial, "am force-stop "+shellQuote(pkg))
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined, "OK")), nil
}

// currentUserID returns the foreground Android user id (digits), default "0".
func (c *Client) currentUserID(serial string) string {
	res, err := c.Shell(serial, "am get-current-user")
	if err != nil {
		return "0"
	}
	u := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined))
	if u == "" {
		return "0"
	}
	for _, r := range u {
		if r < '0' || r > '9' {
			return "0"
		}
	}
	return u
}

// DisablePackage disables an app for the current user (no root on most devices).
// Tries disable-user / cmd package / pm disable with the active user id.
// Many Xiaomi/HyperOS preloads reject shell disable (SecurityException); callers may
// fall back to pm uninstall --user <id> (current-user uninstall).
func (c *Client) DisablePackage(serial, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", fmt.Errorf("包名为空")
	}
	q := shellQuote(pkg)
	user := c.currentUserID(serial)
	cmds := []string{
		"pm disable-user --user " + user + " " + q,
		"pm disable-user " + q,
		"cmd package disable-user --user " + user + " " + q,
		"pm disable --user " + user + " " + q,
		"pm disable " + q,
	}
	var last string
	for _, cmd := range cmds {
		res, err := c.Shell(serial, cmd)
		out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Stderr, res.Combined))
		if out != "" {
			last = out
		} else if err != nil {
			last = err.Error()
		}
		if err == nil && !isPmStateFailure(out, "disabled") {
			return out, nil
		}
	}
	if isProtectedDisableError(last) {
		return last, fmt.Errorf(
			"系统拒绝 shell 禁用该应用（常见于小米/HyperOS 预装保护包，如超级小爱）。\n"+
				"可改用「卸载（当前用户）」：pm uninstall --user %s，多数机型无需 root，应用会出现在「已卸载」列表。\n\n%s",
			user, last)
	}
	return last, fmt.Errorf("%s", firstNonEmpty(last, "禁用失败"))
}

// restoreCompanionPkgs returns related system packages that often must be restored together.
// Only packages that exist on the device (pm path / residual) are actually restored.
func restoreCompanionPkgs(pkg string) []string {
	pkg = strings.TrimSpace(pkg)
	switch pkg {
	case "com.miui.voiceassist":
		// 超级小爱：主程序 + 语音唤醒 + 澎湃 AI 等常见依赖
		return []string{
			"com.miui.voiceassist",
			"com.miui.voicetrigger",
			"com.xiaomi.aicr",
			"com.miui.personalassistant",
			"com.xiaomi.aiasst.service",
			"com.xiaomi.aiasst.vision",
		}
	default:
		return []string{pkg}
	}
}

// restoreOnePackage runs install-existing + enable + force-stop for a single package.
// ok means the package is usable for the current user (installed/enabled), not that every subcommand succeeded.
func (c *Client) restoreOnePackage(serial, pkg, user string) (ok bool, detail string) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return false, "包名为空"
	}
	q := shellQuote(pkg)
	var parts []string

	// Does the APK exist on the device at all (including residual after user-uninstall)?
	pathRes, _ := c.Shell(serial, "pm path "+q+" 2>/dev/null; pm path --user "+user+" "+q+" 2>/dev/null")
	pathOut := strings.TrimSpace(firstNonEmpty(pathRes.Stdout, pathRes.Combined))
	// Also check residual list
	listRes, _ := c.Shell(serial, "pm list packages -u "+q)
	listOut := strings.TrimSpace(firstNonEmpty(listRes.Stdout, listRes.Combined))
	exists := strings.Contains(pathOut, "package:") || strings.Contains(listOut, "package:"+pkg)
	if !exists {
		// Try install-existing anyway (some builds only answer after this).
		resIE, errIE := c.Shell(serial, "cmd package install-existing --user "+user+" "+q)
		outIE := strings.TrimSpace(firstNonEmpty(resIE.Stdout, resIE.Stderr, resIE.Combined))
		if errIE != nil || isPmStateFailure(outIE, "enabled") {
			if outIE == "" && errIE != nil {
				outIE = errIE.Error()
			}
			return false, "设备上无此系统包，无法恢复: " + firstNonEmpty(outIE, "not found")
		}
		parts = append(parts, firstNonEmpty(outIE, "install-existing OK"))
	} else {
		resIE, errIE := c.Shell(serial, "cmd package install-existing --user "+user+" "+q)
		outIE := strings.TrimSpace(firstNonEmpty(resIE.Stdout, resIE.Stderr, resIE.Combined))
		if errIE == nil && !isPmStateFailure(outIE, "enabled") {
			parts = append(parts, firstNonEmpty(outIE, "install-existing OK"))
		} else if outIE != "" {
			parts = append(parts, "install-existing: "+outIE)
		}
	}

	enabled := false
	for _, cmd := range []string{
		"pm enable " + q,
		"pm enable --user " + user + " " + q,
		"cmd package enable --user " + user + " " + q,
	} {
		res, err := c.Shell(serial, cmd)
		out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Stderr, res.Combined))
		if err == nil && !isPmStateFailure(out, "enabled") {
			parts = append(parts, firstNonEmpty(out, "enable OK"))
			enabled = true
			break
		}
	}
	if !enabled {
		// install-existing success is enough for residual packages on many devices.
		parts = append(parts, "enable 跳过/受限（若已 install-existing 可忽略）")
	}

	_, _ = c.Shell(serial, "am force-stop "+q)

	// Final presence check for current user.
	check, _ := c.Shell(serial, "pm path "+q)
	checkOut := strings.TrimSpace(firstNonEmpty(check.Stdout, check.Combined))
	ok = strings.Contains(checkOut, "package:") || enabled
	if ok {
		parts = append(parts, "状态: 当前用户可见")
	} else {
		parts = append(parts, "状态: 仍不可见")
	}
	return ok, strings.Join(parts, "; ")
}

// EnablePackage re-enables a disabled app, or restores a user-uninstalled system app.
// For known multi-package suites (e.g. 超级小爱), also restores companion packages.
func (c *Client) EnablePackage(serial, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", fmt.Errorf("包名为空")
	}
	user := c.currentUserID(serial)
	targets := restoreCompanionPkgs(pkg)
	var lines []string
	mainOK := false

	for _, p := range targets {
		ok, detail := c.restoreOnePackage(serial, p, user)
		tag := "失败"
		if ok {
			tag = "OK"
			if p == pkg {
				mainOK = true
			}
		} else if p != pkg {
			// Companion missing on this ROM is normal — report as skip, not hard fail.
			if strings.Contains(detail, "无此系统包") {
				tag = "跳过"
			}
		}
		if p == pkg && ok {
			mainOK = true
		}
		lines = append(lines, fmt.Sprintf("[%s] %s — %s", tag, p, detail))
	}

	// Try launch main package once (best-effort).
	if mainOK {
		if out, err := c.LaunchPackage(serial, pkg); err == nil {
			lines = append(lines, "已尝试启动: "+out)
		} else if out != "" {
			lines = append(lines, "启动尝试: "+out)
		}
	}

	lines = append(lines, "",
		"若超级小爱仍无法语音唤醒/打开：",
		"1. 设置 → 应用设置 → 管理应用 → 超级小爱 → 权限全部允许，并「清除缓存」后再开",
		"2. 设置 → 更多设置 / 应用设置 → 默认应用 → 数字助理/语音助手 → 选超级小爱",
		"3. 确认关联包 com.miui.voicetrigger（语音唤醒）上方为 OK；若失败请手动恢复该包",
		"4. 仍不行：设置 → 应用设置 → 超级小爱 → 卸载更新（若有）后重启，或系统更新/应用商店修复",
	)

	if !mainOK {
		return strings.Join(lines, "\n"), fmt.Errorf("主包 %s 未能恢复\n%s", pkg, strings.Join(lines, "\n"))
	}
	return strings.Join(lines, "\n"), nil
}

// DiagnosePackage returns a short text report of package install/enable state (for support).
func (c *Client) DiagnosePackage(serial, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", fmt.Errorf("包名为空")
	}
	user := c.currentUserID(serial)
	q := shellQuote(pkg)
	var b strings.Builder
	fmt.Fprintf(&b, "包名: %s\n用户: %s\n", pkg, user)

	run := func(title, cmd string) {
		res, err := c.Shell(serial, cmd)
		out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Stderr, res.Combined))
		if out == "" && err != nil {
			out = err.Error()
		}
		if out == "" {
			out = "(空)"
		}
		// keep report short
		if len(out) > 800 {
			out = out[:800] + "…"
		}
		fmt.Fprintf(&b, "\n## %s\n%s\n", title, out)
	}

	run("pm path", "pm path "+q)
	run("pm list packages -u (含已卸载残留)", "pm list packages -u "+q)
	run("pm list packages -d (若在禁用列表)", "pm list packages -d "+q)
	run("dumpsys 启用状态", "dumpsys package "+q+" | grep -E 'User |enabled=|installed=|hidden=|stopped=|ceDataInode' | head -n 40")
	run("可启动 Activity", "cmd package resolve-activity --brief -c android.intent.category.LAUNCHER "+q)

	// companions snapshot
	if comps := restoreCompanionPkgs(pkg); len(comps) > 1 {
		fmt.Fprintf(&b, "\n## 关联包是否存在\n")
		for _, p := range comps {
			res, _ := c.Shell(serial, "pm path "+shellQuote(p)+" 2>/dev/null; pm list packages -u "+shellQuote(p))
			out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined))
			if strings.Contains(out, "package:") {
				fmt.Fprintf(&b, "OK  %s\n", p)
			} else {
				fmt.Fprintf(&b, "缺  %s\n", p)
			}
		}
	}
	return b.String(), nil
}

// isProtectedDisableError detects vendor-blocked shell disable (esp. MIUI/HyperOS).
func isProtectedDisableError(out string) bool {
	low := strings.ToLower(out)
	return strings.Contains(low, "securityexception") ||
		strings.Contains(low, "security exception") ||
		strings.Contains(low, "cannot change component state") ||
		strings.Contains(low, "shell cannot change")
}

// isPmStateFailure reports whether pm enable/disable output looks like a failure.
// want is "disabled" or "enabled" (substring of "new state: ...").
func isPmStateFailure(out, want string) bool {
	low := strings.ToLower(out)
	if low == "" {
		return false
	}
	// Success typically: "Package xxx new state: disabled-user" / "enabled"
	if strings.Contains(low, "new state:") && strings.Contains(low, strings.ToLower(want)) {
		return false
	}
	// install-existing success: "Package com.xxx installed for user: 0"
	if strings.Contains(low, "installed for user") {
		return false
	}
	return strings.Contains(low, "error") ||
		strings.Contains(low, "exception") ||
		strings.Contains(low, "securityexception") ||
		strings.Contains(low, "security exception") ||
		strings.Contains(low, "cannot change component state") ||
		strings.Contains(low, "not allowed") ||
		strings.Contains(low, "failed") ||
		strings.Contains(low, "does not exist") ||
		strings.Contains(low, "unknown package")
}

// IsPackageDebuggable reports whether the package was built as debuggable.
// Logic: dumpsys package <pkg> | grep -E "flags|DEBUGGABLE" — true if output contains DEBUGGABLE.
func (c *Client) IsPackageDebuggable(serial, pkg string) (bool, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return false, fmt.Errorf("包名为空")
	}
	// Device-side filter (same approach as requested); grep exit 1 on no match is OK.
	cmd := "dumpsys package " + shellQuote(pkg) + ` | grep -E 'flags|DEBUGGABLE'`
	res, err := c.Shell(serial, cmd)
	out := firstNonEmpty(res.Stdout, res.Combined, res.Stderr)
	if strings.Contains(out, "DEBUGGABLE") {
		return true, nil
	}
	// No DEBUGGABLE in filtered output → not debug. Ignore grep's non-zero exit.
	if err != nil && strings.TrimSpace(out) == "" {
		// dumpsys itself may have failed; try unfiltered one-shot for clearer signal
		res2, err2 := c.Shell(serial, "dumpsys package "+shellQuote(pkg))
		out2 := firstNonEmpty(res2.Stdout, res2.Combined)
		if err2 != nil && strings.TrimSpace(out2) == "" {
			return false, fmt.Errorf("%s", firstNonEmpty(out, err.Error(), "无法读取包信息"))
		}
		return strings.Contains(out2, "DEBUGGABLE"), nil
	}
	return false, nil
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

// ---------- version resolution ----------

type pkgVersion struct {
	Name string
	Code int64
}

var (
	rePkgHeader   = regexp.MustCompile(`^\s*Package\s+\[([^\]]+)\]`)
	reVersionCode = regexp.MustCompile(`^\s*versionCode=(\d+)`)
	reVersionName = regexp.MustCompile(`^\s*versionName=(.*)$`)
)

// fillVersions populates VersionName / VersionCode via dumpsys package (bulk).
// Falls back to `pm list packages --show-versioncode` (code only) if dumpsys fails.
func (c *Client) fillVersions(serial string, list []PackageInfo) {
	if len(list) == 0 {
		return
	}
	vers := c.fetchVersionsDumpsys(serial)
	if len(vers) == 0 {
		vers = c.fetchVersionCodesOnly(serial)
	}
	if len(vers) == 0 {
		return
	}
	applyPackageVersions(list, vers)
}

func applyPackageVersions(list []PackageInfo, vers map[string]pkgVersion) {
	for i := range list {
		if v, ok := vers[list[i].Name]; ok {
			if v.Name != "" {
				list[i].VersionName = v.Name
			}
			if v.Code != 0 {
				list[i].VersionCode = v.Code
			}
		}
	}
}

func (c *Client) fetchVersionsDumpsys(serial string) map[string]pkgVersion {
	// Device-side filter keeps adb payload small; full dumpsys is multi-MB.
	// toybox/busybox grep both accept -E on modern Android.
	const filtered = `dumpsys package 2>/dev/null | grep -E '^[[:space:]]*Package \[|^[[:space:]]*versionCode=|^[[:space:]]*versionName='`
	res, err := c.RunTimeout(serial, 2*time.Minute, "shell", filtered)
	out := ""
	if err == nil {
		out = res.Stdout
	}
	if strings.TrimSpace(out) == "" || !strings.Contains(out, "Package [") {
		// Fallback: full dumpsys (slower / larger).
		res2, err2 := c.RunTimeout(serial, 2*time.Minute, "shell", "dumpsys", "package")
		if err2 != nil || strings.TrimSpace(res2.Stdout) == "" {
			return nil
		}
		out = res2.Stdout
	}
	return parseDumpsysPackageVersions(out)
}

func (c *Client) fetchVersionCodesOnly(serial string) map[string]pkgVersion {
	res, err := c.RunTimeout(serial, 60*time.Second, "shell", "pm", "list", "packages", "--show-versioncode")
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return nil
	}
	out := make(map[string]pkgVersion)
	// package:com.example versionCode:123
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		body := strings.TrimPrefix(line, "package:")
		name := body
		var code int64
		if i := strings.Index(body, " "); i > 0 {
			name = body[:i]
			rest := strings.TrimSpace(body[i+1:])
			if strings.HasPrefix(rest, "versionCode:") {
				code, _ = strconv.ParseInt(strings.TrimPrefix(rest, "versionCode:"), 10, 64)
			}
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = pkgVersion{Code: code}
	}
	return out
}

func parseDumpsysPackageVersions(dumpsys string) map[string]pkgVersion {
	out := make(map[string]pkgVersion)
	var cur string
	var ver pkgVersion
	flush := func() {
		if cur == "" {
			return
		}
		// Keep first block per package name (primary user package entry).
		if _, exists := out[cur]; !exists {
			out[cur] = ver
		}
		cur = ""
		ver = pkgVersion{}
	}
	for _, line := range strings.Split(dumpsys, "\n") {
		if m := rePkgHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = m[1]
			continue
		}
		if cur == "" {
			continue
		}
		if m := reVersionCode.FindStringSubmatch(line); m != nil {
			if ver.Code == 0 {
				ver.Code, _ = strconv.ParseInt(m[1], 10, 64)
			}
			continue
		}
		if m := reVersionName.FindStringSubmatch(line); m != nil {
			if ver.Name == "" {
				ver.Name = strings.TrimSpace(m[1])
			}
			continue
		}
	}
	flush()
	return out
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
