package logcat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"adbsuite/internal/procutil"
)

type Streamer struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	serial string
}

func New() *Streamer {
	return &Streamer{}
}

func (s *Streamer) Start(adbPath, serial string, clearFirst bool, onLine func(string), onStop func(reason, message string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return fmt.Errorf("logcat 已在运行")
	}
	if clearFirst {
		cargs := []string{}
		if serial != "" {
			cargs = append(cargs, "-s", serial)
		}
		cargs = append(cargs, "logcat", "-c")
		c := exec.Command(adbPath, cargs...)
		procutil.HideConsole(c)
		_ = c.Run()
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "logcat", "-v", "time")
	cmd := exec.CommandContext(ctx, adbPath, args...)
	procutil.HideConsole(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	s.cmd = cmd
	s.cancel = cancel
	s.serial = serial
	go func() {
		reader := bufio.NewReaderSize(stdout, 64*1024)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				// trim trailing newline
				for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
					line = line[:len(line)-1]
				}
				if onLine != nil {
					onLine(line)
				}
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					if onStop != nil {
						onStop("error", err.Error())
					}
				} else if onStop != nil {
					if ctx.Err() != nil {
						onStop("user", "")
					} else {
						onStop("disconnected", "logcat 进程已结束")
					}
				}
				break
			}
		}
		_ = cmd.Wait()
		s.mu.Lock()
		s.cmd = nil
		s.cancel = nil
		s.serial = ""
		s.mu.Unlock()
	}()
	return nil
}

func (s *Streamer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.cmd = nil
	s.cancel = nil
	s.serial = ""
}

func (s *Streamer) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil
}

func (s *Streamer) Serial() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serial
}
