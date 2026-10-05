package adb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"adbsuite/internal/procutil"
)

type Client struct {
	AdbPath func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Code     int    `json:"code"`
	Combined string `json:"combined"`
}

func (c *Client) path() string {
	if c.AdbPath != nil {
		return c.AdbPath()
	}
	return "adb"
}

func (c *Client) Run(ctx context.Context, serial string, args ...string) (Result, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
	}
	cmdArgs := make([]string, 0, len(args)+2)
	if serial != "" {
		cmdArgs = append(cmdArgs, "-s", serial)
	}
	cmdArgs = append(cmdArgs, args...)
	command := c.command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(ctx, c.path(), cmdArgs...)
	procutil.HideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   0,
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
		} else {
			res.Code = -1
			if res.Stderr == "" {
				res.Stderr = err.Error()
			}
			res.Combined = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
			return res, fmt.Errorf("%s", firstNonEmpty(res.Stderr, err.Error()))
		}
	}
	res.Combined = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if res.Code != 0 {
		return res, fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(res.Stderr), strings.TrimSpace(res.Stdout), fmt.Sprintf("exit %d", res.Code)))
	}
	return res, nil
}

func (c *Client) RunTimeout(serial string, timeout time.Duration, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Run(ctx, serial, args...)
}

func (c *Client) Version() (string, error) {
	res, err := c.RunTimeout("", 8*time.Second, "version")
	if err != nil {
		return "", fmt.Errorf("无法执行 adb（路径: %s）: %w", c.path(), err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (c *Client) Shell(serial, command string) (Result, error) {
	// pass as single argument so device shell parses it
	return c.RunTimeout(serial, 60*time.Second, "shell", command)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return "unknown error"
}
