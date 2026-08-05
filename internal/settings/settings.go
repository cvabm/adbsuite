package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	AdbPath         string   `json:"adbPath"`
	ScrcpyPath      string   `json:"scrcpyPath"`
	Theme           string   `json:"theme"` // dark | light
	MaxConcurrency  int      `json:"maxConcurrency"`
	ScrcpyMaxSize   int      `json:"scrcpyMaxSize"`
	ScrcpyBitRate   string   `json:"scrcpyBitRate"`
	ScrcpyMaxFps    int      `json:"scrcpyMaxFps"`
	ScrcpyStayAwake bool     `json:"scrcpyStayAwake"`
	ScrcpyNoAudio   bool     `json:"scrcpyNoAudio"`
	KillScrcpyOnExit bool    `json:"killScrcpyOnExit"`
	RecentHosts     []string `json:"recentHosts"`
	LastDevice      string   `json:"lastDevice"`
	TcpipPort       int      `json:"tcpipPort"`
}

func Default() Settings {
	return Settings{
		Theme:            "light",
		MaxConcurrency:   4,
		ScrcpyMaxSize:    0,
		ScrcpyBitRate:    "8M",
		ScrcpyMaxFps:     0,
		ScrcpyStayAwake:  true,
		ScrcpyNoAudio:    false,
		KillScrcpyOnExit: true,
		RecentHosts:      []string{},
		TcpipPort:        5555,
	}
}

type Store struct {
	mu   sync.RWMutex
	path string
	cur  Settings
}

func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	dir = filepath.Join(dir, "adbsuite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "settings.json"), cur: Default()}
	_ = s.Load()
	return s, nil
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cur Settings
	if err := json.Unmarshal(data, &cur); err != nil {
		return err
	}
	// fill zeros with defaults
	def := Default()
	if cur.Theme == "" {
		cur.Theme = def.Theme
	}
	if cur.MaxConcurrency <= 0 {
		cur.MaxConcurrency = def.MaxConcurrency
	}
	if cur.TcpipPort <= 0 {
		cur.TcpipPort = def.TcpipPort
	}
	if cur.RecentHosts == nil {
		cur.RecentHosts = []string{}
	}
	s.cur = cur
	return nil
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *Store) Save(cur Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur.MaxConcurrency <= 0 {
		cur.MaxConcurrency = 4
	}
	if cur.TcpipPort <= 0 {
		cur.TcpipPort = 5555
	}
	if cur.RecentHosts == nil {
		cur.RecentHosts = []string{}
	}
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o644); err != nil {
		return err
	}
	s.cur = cur
	return nil
}

func (s *Store) AddRecentHost(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	host = filepath.ToSlash(host)
	list := make([]string, 0, len(s.cur.RecentHosts)+1)
	list = append(list, host)
	for _, h := range s.cur.RecentHosts {
		if h != host {
			list = append(list, h)
		}
	}
	if len(list) > 12 {
		list = list[:12]
	}
	s.cur.RecentHosts = list
	data, _ := json.MarshalIndent(s.cur, "", "  ")
	_ = os.WriteFile(s.path, data, 0o644)
}
