package logcat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"adbsuite/internal/procutil"
)

type Streamer struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	serial  string
	command func(context.Context, string, ...string) *exec.Cmd
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
	command := s.command
	if command == nil {
		command = exec.CommandContext
	}
	if clearFirst {
		cargs := []string{}
		if serial != "" {
			cargs = append(cargs, "-s", serial)
		}
		cargs = append(cargs, "logcat", "-c")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := command(ctx, adbPath, cargs...)
		procutil.HideConsole(c)
		if output, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("清除 logcat 失败: %w；%s", err, output)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "logcat", "-v", "time")
	cmd := command(ctx, adbPath, args...)
	cmd.WaitDelay = 2 * time.Second
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
	// Closing the read pipe on cancellation also handles inherited child handles.
	closeRead := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	go func() {
		defer cancel()
		defer closeRead()
		reason, message := "disconnected", "logcat 进程已结束"
		reader := bufio.NewReaderSize(stdout, 64*1024)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				// trim trailing newline
				for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
					line = line[:len(line)-1]
				}
				if onLine != nil && ctx.Err() == nil {
					onLine(line)
				}
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					reason, message = "error", err.Error()
				} else {
					if ctx.Err() != nil {
						reason, message = "user", ""
					}
				}
				break
			}
		}
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			reason, message = "error", err.Error()
		}
		if s.finishProcess(cmd) && onStop != nil {
			onStop(reason, message)
		}
	}()
	return nil
}

func (s *Streamer) finishProcess(cmd *exec.Cmd) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != cmd {
		return false
	}
	s.cmd = nil
	s.cancel = nil
	s.serial = ""
	return true
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
