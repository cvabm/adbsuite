package adb

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// RemoteEntry is one filesystem entry on the device (Device Explorer row).
type RemoteEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsDir  bool   `json:"isDir"`
	IsLink bool   `json:"isLink"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode"`  // e.g. drwxr-xr-x
	MTime  string `json:"mtime"` // display string from ls
	Link   string `json:"link,omitempty"`
}

func (c *Client) Push(serial, local, remote string) (string, error) {
	res, err := c.RunTimeout(serial, 30*time.Minute, "push", local, remote)
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Combined), nil
}

func (c *Client) Pull(serial, remote, local string) (string, error) {
	res, err := c.RunTimeout(serial, 30*time.Minute, "pull", remote, local)
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Combined), nil
}

// ListDir returns a text listing (legacy helper). Prefer ListDirEntries.
func (c *Client) ListDir(serial, remote string) (string, error) {
	entries, err := c.ListDirEntries(serial, remote)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s %8d %s %s\n", e.Mode, e.Size, e.MTime, e.Name)
	}
	return b.String(), nil
}

// ListDirEntries lists a remote directory like Android Studio Device Explorer.
func (c *Client) ListDirEntries(serial, remote string) ([]RemoteEntry, error) {
	remote = normalizeRemotePath(remote)
	res, err := c.Shell(serial, "ls -la "+shellQuote(remote))
	out := firstNonEmpty(res.Stdout, res.Stderr, res.Combined)
	if err != nil {
		if strings.TrimSpace(out) != "" {
			return nil, fmt.Errorf("%s", strings.TrimSpace(out))
		}
		return nil, err
	}
	entries, parseErr := parseLsLa(remote, res.Stdout)
	if parseErr != nil {
		res2, err2 := c.Shell(serial, "ls -a "+shellQuote(remote))
		if err2 != nil {
			return nil, fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(res2.Combined), err2.Error()))
		}
		return parseLsNames(remote, res2.Stdout), nil
	}
	return entries, nil
}

func (c *Client) Mkdir(serial, remote string) error {
	remote = normalizeRemotePath(remote)
	if remote == "" || remote == "/" {
		return fmt.Errorf("无效路径")
	}
	res, err := c.Shell(serial, "mkdir -p "+shellQuote(remote))
	if err != nil {
		return fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(res.Combined), err.Error()))
	}
	return nil
}

// DeleteRemote removes a file or directory. recursive uses rm -rf.
func (c *Client) DeleteRemote(serial, remote string, recursive bool) error {
	remote = normalizeRemotePath(remote)
	if remote == "" || remote == "/" {
		return fmt.Errorf("拒绝删除根目录")
	}
	var cmd string
	if recursive {
		cmd = "rm -rf " + shellQuote(remote)
	} else {
		cmd = "rm -f " + shellQuote(remote)
	}
	res, err := c.Shell(serial, cmd)
	if err != nil {
		return fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(res.Combined), err.Error()))
	}
	return nil
}

func (c *Client) RenameRemote(serial, oldPath, newPath string) error {
	oldPath = normalizeRemotePath(oldPath)
	newPath = normalizeRemotePath(newPath)
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("路径为空")
	}
	if oldPath == "/" || newPath == "/" {
		return fmt.Errorf("拒绝操作根目录")
	}
	res, err := c.Shell(serial, "mv "+shellQuote(oldPath)+" "+shellQuote(newPath))
	if err != nil {
		return fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(res.Combined), err.Error()))
	}
	return nil
}

func normalizeRemotePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/sdcard"
	}
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if p == "" {
		return "/"
	}
	return p
}

func joinRemote(dir, name string) string {
	dir = normalizeRemotePath(dir)
	name = strings.Trim(name, "/")
	if name == "" || name == "." {
		return dir
	}
	if name == ".." {
		return parentRemote(dir)
	}
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

func parentRemote(p string) string {
	p = normalizeRemotePath(p)
	if p == "/" {
		return "/"
	}
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func looksLikeErrorLine(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "permission denied") ||
		strings.Contains(low, "no such file") ||
		strings.Contains(low, "not a directory") ||
		strings.Contains(low, "cannot access") ||
		strings.Contains(low, "not found")
}

func parseLsLa(dir, stdout string) ([]RemoteEntry, error) {
	var entries []RemoteEntry
	parsed := 0
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "total ") {
			continue
		}
		if f := strings.Fields(trim); looksLikeErrorLine(trim) && (len(f) == 0 || !isModeToken(f[0])) {
			continue
		}
		e, ok := parseLsLine(dir, line)
		if !ok {
			continue
		}
		if e.Name == "." || e.Name == ".." {
			continue
		}
		entries = append(entries, e)
		parsed++
	}
	if parsed == 0 {
		trim := strings.TrimSpace(stdout)
		if trim != "" && !strings.HasPrefix(trim, "total ") {
			fields := strings.Fields(trim)
			if len(fields) > 0 && isModeToken(fields[0]) {
				return nil, fmt.Errorf("无法解析目录列表")
			}
		}
	}
	sortRemoteEntries(entries)
	return entries, nil
}

func parseLsNames(dir, stdout string) []RemoteEntry {
	var entries []RemoteEntry
	for _, line := range strings.Split(stdout, "\n") {
		name := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if name == "" || name == "." || name == ".." {
			continue
		}
		if looksLikeErrorLine(name) {
			continue
		}
		entries = append(entries, RemoteEntry{
			Name: name,
			Path: joinRemote(dir, name),
			Mode: "?",
		})
	}
	sortRemoteEntries(entries)
	return entries
}

func sortRemoteEntries(entries []RemoteEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// parseLsLine parses one long-format line from toybox/busybox/coreutils ls -la.
func parseLsLine(dir, line string) (RemoteEntry, bool) {
	line = strings.TrimRight(line, "\r")
	trim := strings.TrimSpace(line)
	if trim == "" {
		return RemoteEntry{}, false
	}
	fields := strings.Fields(trim)
	if len(fields) < 6 {
		return RemoteEntry{}, false
	}
	mode := fields[0]
	if !isModeToken(mode) {
		return RemoteEntry{}, false
	}
	// mode nlink owner group size date… name
	// size is fields[4] for standard layout
	sizeIdx := 4
	if sizeIdx >= len(fields) || !isIntToken(fields[sizeIdx]) {
		return RemoteEntry{}, false
	}
	size, _ := strconv.ParseInt(fields[sizeIdx], 10, 64)

	dateStart := sizeIdx + 1
	dateEnd, mtime := detectDateSpan(fields, dateStart)
	if dateEnd <= dateStart || dateEnd >= len(fields) {
		return RemoteEntry{}, false
	}

	// Name may contain spaces — take from original line after the last date token.
	namePart := nameFromLine(trim, fields, dateEnd)
	if namePart == "" {
		namePart = strings.Join(fields[dateEnd:], " ")
	}

	name := namePart
	link := ""
	isLink := strings.HasPrefix(mode, "l")
	isDir := strings.HasPrefix(mode, "d")
	if isLink {
		if i := strings.Index(namePart, " -> "); i >= 0 {
			name = namePart[:i]
			link = namePart[i+4:]
		}
	}
	if name == "" {
		return RemoteEntry{}, false
	}
	return RemoteEntry{
		Name:   name,
		Path:   joinRemote(dir, name),
		IsDir:  isDir,
		IsLink: isLink,
		Size:   size,
		Mode:   mode,
		MTime:  mtime,
		Link:   link,
	}, true
}

func isModeToken(s string) bool {
	if len(s) < 10 {
		return false
	}
	switch s[0] {
	case 'd', '-', 'l', 'b', 'c', 'p', 's':
		return true
	default:
		return false
	}
}

func isIntToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// detectDateSpan returns exclusive end index of date tokens and a display mtime.
func detectDateSpan(fields []string, start int) (end int, mtime string) {
	if start >= len(fields) {
		return start, ""
	}
	// ISO: 2024-01-01 12:00[:ss[.nanos][+tz]]
	if looksISODate(fields[start]) {
		if start+1 >= len(fields) {
			return start, ""
		}
		t := stripTimeExtras(fields[start+1])
		return start + 2, fields[start] + " " + t
	}
	// Month: Jan  1 12:00  or Jan  1  2024
	if looksMonth(fields[start]) {
		if start+2 >= len(fields) {
			return start, ""
		}
		return start + 3, fields[start] + " " + fields[start+1] + " " + fields[start+2]
	}
	// Fallback: two tokens
	if start+1 < len(fields) {
		return start + 2, fields[start] + " " + fields[start+1]
	}
	return start, ""
}

// nameFromLine reconstructs the filename (and optional " -> target") preserving spaces.
func nameFromLine(line string, fields []string, nameFieldIdx int) string {
	if nameFieldIdx <= 0 || nameFieldIdx >= len(fields) {
		return ""
	}
	// Walk the line consuming whitespace-separated tokens until nameFieldIdx.
	pos := 0
	for i := 0; i < nameFieldIdx; i++ {
		// skip spaces
		for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
			pos++
		}
		tok := fields[i]
		if pos+len(tok) > len(line) || line[pos:pos+len(tok)] != tok {
			// mismatch — fall back
			return ""
		}
		pos += len(tok)
	}
	for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
		pos++
	}
	return strings.TrimRight(line[pos:], " \t")
}

func looksISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func looksMonth(s string) bool {
	switch s {
	case "Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec":
		return true
	default:
		return false
	}
}

func stripTimeExtras(t string) string {
	// 12:00:00.123456789+0800 → 12:00:00
	if i := strings.IndexByte(t, '.'); i > 0 {
		return t[:i]
	}
	if i := strings.IndexAny(t, "+-"); i > 0 && strings.Contains(t, ":") {
		// careful: time itself has no leading +; timezone may be +0800
		// only strip if after seconds
		if strings.Count(t[:i], ":") >= 1 {
			return t[:i]
		}
	}
	return t
}
