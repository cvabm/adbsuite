package adb

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RunningService is one active Android ServiceRecord from dumpsys activity services.
type RunningService struct {
	Package        string `json:"package"`
	Label          string `json:"label,omitempty"` // human app name
	Service        string `json:"service"`         // short class name (last segment)
	Component      string `json:"component"`       // package/.Class or package/full.Class
	Process        string `json:"process,omitempty"`
	PID            int    `json:"pid,omitempty"`
	UserID         int    `json:"userId"`
	Client         string `json:"client,omitempty"`
	Foreground     bool   `json:"foreground"`
	StartRequested bool   `json:"startRequested"`
	System         bool   `json:"system"`
	CreateTime     string `json:"createTime,omitempty"`
	LastActivity   string `json:"lastActivity,omitempty"`
	BaseDir        string `json:"baseDir,omitempty"`
}

var (
	reServiceRecord = regexp.MustCompile(`ServiceRecord\{[0-9a-fA-F]+\s+u(\d+)\s+([^\s}]+)(?:\s+c:([^\s}]+))?`)
	reProcessRecord = regexp.MustCompile(`ProcessRecord\{[0-9a-fA-F]+\s+(\d+):`)
)

// ListRunningServices runs dumpsys activity services and returns parsed service records.
// System vs third-party is determined via `pm list packages -s` (same as RSM-style categories).
func (c *Client) ListRunningServices(serial string) ([]RunningService, error) {
	res, err := c.RunTimeout(serial, 90*time.Second, "shell", "dumpsys", "activity", "services")
	// dumpsys may exit non-zero on some builds while still printing useful output.
	out := firstNonEmpty(res.Stdout, res.Combined)
	if strings.TrimSpace(out) == "" {
		if err != nil {
			return nil, fmt.Errorf("dumpsys activity services 失败: %w", err)
		}
		return nil, fmt.Errorf("dumpsys activity services 无输出")
	}

	sysPkgs, _ := c.listPackageNames(serial, []string{"-s"})
	list := parseRunningServices(out, sysPkgs)
	c.fillServiceLabels(serial, list)
	return list, nil
}

// fillServiceLabels resolves human-readable app names for unique packages
// (reuses app label cache + APK manifest parsing from apps.go).
func (c *Client) fillServiceLabels(serial string, list []RunningService) {
	if len(list) == 0 {
		return
	}

	// package → best baseDir path + service indices
	type pkgMeta struct {
		path string
		idxs []int
	}
	byPkg := make(map[string]*pkgMeta, 64)
	for i := range list {
		pkg := list[i].Package
		if pkg == "" {
			continue
		}
		m, ok := byPkg[pkg]
		if !ok {
			m = &pkgMeta{}
			byPkg[pkg] = m
		}
		m.idxs = append(m.idxs, i)
		if list[i].BaseDir != "" && m.path == "" {
			m.path = list[i].BaseDir
		}
	}

	// First pass: cache hits (exact path or any path for this package).
	type needResolve struct {
		pkg  string
		path string
	}
	var missing []needResolve
	labels := make(map[string]string, len(byPkg))
	for pkg, m := range byPkg {
		if m.path != "" {
			if lab, ok := getCachedLabel(pkg, m.path); ok {
				labels[pkg] = lab
				continue
			}
		}
		if lab, ok := getCachedLabelAny(pkg); ok {
			labels[pkg] = lab
			continue
		}
		if m.path != "" {
			missing = append(missing, needResolve{pkg: pkg, path: m.path})
		} else {
			labels[pkg] = friendlyFallback(pkg)
		}
	}

	// Resolve remaining via APK (same as apps list; concurrent, modest).
	if len(missing) > 0 {
		const workers = 6
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup
		var mu sync.Mutex
		newEntries := make(map[string]string, len(missing))
		for _, j := range missing {
			j := j
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if recover() != nil {
						mu.Lock()
						fb := friendlyFallback(j.pkg)
						labels[j.pkg] = fb
						newEntries[labelCacheKey(j.pkg, j.path)] = fb
						mu.Unlock()
					}
				}()
				label := c.resolveAppLabel(serial, j.path)
				if label == "" {
					label = friendlyFallback(j.pkg)
				}
				mu.Lock()
				labels[j.pkg] = label
				newEntries[labelCacheKey(j.pkg, j.path)] = label
				mu.Unlock()
			}()
		}
		wg.Wait()
		putCachedLabels(newEntries)
	}

	for pkg, m := range byPkg {
		lab := labels[pkg]
		if lab == "" {
			lab = friendlyFallback(pkg)
		}
		for _, i := range m.idxs {
			list[i].Label = lab
		}
	}
}

// getCachedLabelAny returns any cached label for the package (path may differ).
func getCachedLabelAny(name string) (string, bool) {
	initLabelCache()
	labelCacheMu.Lock()
	defer labelCacheMu.Unlock()
	prefix := name + "\x00"
	for k, v := range labelCacheMem {
		if strings.HasPrefix(k, prefix) && v != "" {
			return v, true
		}
	}
	return "", false
}

// StopService stops a specific service component via am stopservice.
// component should be "package/class" form.
func (c *Client) StopService(serial, component string) (string, error) {
	component = strings.TrimSpace(component)
	if component == "" {
		return "", fmt.Errorf("服务组件为空")
	}
	if !strings.Contains(component, "/") {
		return "", fmt.Errorf("服务组件格式应为 package/class")
	}
	res, err := c.Shell(serial, "am stopservice "+shellQuote(component))
	out := strings.TrimSpace(firstNonEmpty(res.Stdout, res.Combined))
	if err != nil {
		return out, err
	}
	return firstNonEmpty(out, "OK"), nil
}

// parseRunningServices extracts ServiceRecords from dumpsys activity services output.
// sysPkgs marks packages that appear in `pm list packages -s`.
func parseRunningServices(dumpsys string, sysPkgs map[string]struct{}) []RunningService {
	lines := strings.Split(dumpsys, "\n")
	var out []RunningService
	var cur *RunningService
	inServices := false

	flush := func() {
		if cur == nil {
			return
		}
		// Prefer explicit packageName; fall back to component package.
		if cur.Package == "" && cur.Component != "" {
			if i := strings.Index(cur.Component, "/"); i > 0 {
				cur.Package = cur.Component[:i]
			}
		}
		if cur.Service == "" && cur.Component != "" {
			cur.Service = shortServiceName(cur.Component)
		}
		if cur.Package != "" {
			if _, ok := sysPkgs[cur.Package]; ok {
				cur.System = true
			} else if isSystemAPKPath(cur.BaseDir) {
				cur.System = true
			}
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)

		// Bound the "active services" section.
		if strings.Contains(trim, "ACTIVITY MANAGER SERVICES") ||
			strings.HasPrefix(trim, "User ") && strings.Contains(trim, "active services") {
			inServices = true
			continue
		}
		if inServices {
			// End of active services block.
			if strings.HasPrefix(trim, "Connection bindings to services") ||
				strings.HasPrefix(trim, "ACTIVITY MANAGER ") && !strings.Contains(trim, "SERVICES") ||
				strings.HasPrefix(trim, "Total number of") {
				flush()
				// Keep going in case multi-user sections reappear later;
				// Connection bindings is the real end of records.
				if strings.HasPrefix(trim, "Connection bindings to services") {
					inServices = false
				}
				continue
			}
		}

		// Some OEM dumpsys omit the banner; still accept ServiceRecord lines.
		if m := reServiceRecord.FindStringSubmatch(line); m != nil {
			flush()
			inServices = true
			uid, _ := strconv.Atoi(m[1])
			comp := strings.TrimSpace(m[2])
			client := ""
			if len(m) > 3 {
				client = strings.TrimSpace(m[3])
			}
			svc := &RunningService{
				Component: comp,
				UserID:    uid,
				Client:    client,
				Service:   shortServiceName(comp),
			}
			if i := strings.Index(comp, "/"); i > 0 {
				svc.Package = comp[:i]
			}
			cur = svc
			continue
		}

		if cur == nil {
			continue
		}

		// app=ProcessRecord{… PID:name/uid}
		if strings.Contains(line, "app=ProcessRecord{") {
			if pm := reProcessRecord.FindStringSubmatch(line); pm != nil {
				if pid, err := strconv.Atoi(pm[1]); err == nil {
					cur.PID = pid
				}
			}
			// Also try to get process name from ProcessRecord if processName missing later.
			// Format: PID:processName/uid
			if idx := strings.Index(line, ":"); idx >= 0 {
				rest := line[idx+1:]
				if slash := strings.Index(rest, "/"); slash > 0 {
					name := strings.TrimSpace(rest[:slash])
					if cur.Process == "" && name != "" {
						cur.Process = name
					}
				}
			}
			continue
		}

		// Key=value fields (may share a line: "startRequested=true delayedStop=false ...")
		// Also: "isForeground=true foregroundId=1 ..."
		// And: "createTime=-5h lastActivity=-1m ..."
		parseServiceFields(line, cur)
	}
	flush()

	// Sort: third-party first, then package, then service.
	sort.Slice(out, func(i, j int) bool {
		if out[i].System != out[j].System {
			return !out[i].System && out[j].System
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Component < out[j].Component
	})
	return out
}

func parseServiceFields(line string, cur *RunningService) {
	// Fast path for known single-token keys that may share a line.
	// Walk space-separated tokens that look like key=value.
	// But values can contain spaces inside [...] so only handle simple tokens here,
	// plus dedicated prefixes for known fields.

	trim := strings.TrimSpace(line)
	if trim == "" {
		return
	}

	// Dedicated prefixes (most reliable).
	switch {
	case strings.HasPrefix(trim, "packageName="):
		cur.Package = strings.TrimSpace(strings.TrimPrefix(trim, "packageName="))
		return
	case strings.HasPrefix(trim, "processName="):
		cur.Process = strings.TrimSpace(strings.TrimPrefix(trim, "processName="))
		return
	case strings.HasPrefix(trim, "baseDir="):
		cur.BaseDir = strings.TrimSpace(strings.TrimPrefix(trim, "baseDir="))
		return
	case strings.HasPrefix(trim, "createTime="):
		// createTime=-5h6m46s523ms startingBgTimeout=--
		rest := strings.TrimPrefix(trim, "createTime=")
		cur.CreateTime = firstToken(rest)
		// also lastActivity may be on same line in some builds — handled below
	case strings.HasPrefix(trim, "isForeground="):
		// fall through to multi-kv
	case strings.HasPrefix(trim, "startRequested="):
		// fall through
	case strings.HasPrefix(trim, "lastActivity="):
		rest := strings.TrimPrefix(trim, "lastActivity=")
		cur.LastActivity = firstToken(rest)
		// may continue with restartTime=...
	case strings.HasPrefix(trim, "intent="):
		return
	}

	// Parse simple key=value tokens on the line.
	// Split carefully: only tokens matching ^key=value without spaces in value.
	fields := splitSimpleKV(trim)
	for k, v := range fields {
		switch k {
		case "packageName":
			if cur.Package == "" {
				cur.Package = v
			}
		case "processName":
			if cur.Process == "" {
				cur.Process = v
			}
		case "baseDir":
			if cur.BaseDir == "" {
				cur.BaseDir = v
			}
		case "isForeground":
			cur.Foreground = v == "true"
		case "startRequested":
			cur.StartRequested = v == "true"
		case "createTime":
			if cur.CreateTime == "" {
				cur.CreateTime = v
			}
		case "lastActivity":
			if cur.LastActivity == "" {
				cur.LastActivity = v
			}
		}
	}
}

// splitSimpleKV extracts key=value pairs where value has no spaces.
func splitSimpleKV(s string) map[string]string {
	out := make(map[string]string)
	// Scan for word=nonspace
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			// skip token
			if sp := strings.IndexAny(s, " \t"); sp >= 0 {
				s = s[sp+1:]
				continue
			}
			break
		}
		key := s[:eq]
		// key should be identifier
		if !isIdent(key) {
			if sp := strings.IndexAny(s, " \t"); sp >= 0 {
				s = s[sp+1:]
				continue
			}
			break
		}
		rest := s[eq+1:]
		// value ends at space, unless bracketed
		var val string
		if strings.HasPrefix(rest, "[") {
			// skip bracketed values for our simple fields
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				break
			}
			s = rest[end+1:]
			continue
		}
		if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
			val = rest[:sp]
			s = rest[sp+1:]
		} else {
			val = rest
			s = ""
		}
		out[key] = val
	}
	return out
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func shortServiceName(component string) string {
	// package/.Foo$Bar or package/com.example.Foo
	slash := strings.Index(component, "/")
	if slash < 0 || slash+1 >= len(component) {
		return component
	}
	cls := component[slash+1:]
	if strings.HasPrefix(cls, ".") {
		cls = cls[1:]
	}
	// last segment after '.'
	if i := strings.LastIndex(cls, "."); i >= 0 {
		cls = cls[i+1:]
	}
	return cls
}
