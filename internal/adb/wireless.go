package adb

import (
	"fmt"
	"strings"
	"time"
)

func (c *Client) Tcpip(serial string, port int) (string, error) {
	if port <= 0 {
		port = 5555
	}
	res, err := c.RunTimeout(serial, 15*time.Second, "tcpip", fmt.Sprintf("%d", port))
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Combined), nil
}

func (c *Client) Connect(host string) (string, error) {
	host = strings.TrimSpace(host)
	res, err := c.RunTimeout("", 20*time.Second, "connect", host)
	out := strings.TrimSpace(res.Combined)
	if err != nil {
		return out, err
	}
	// adb may return 0 but "failed to connect"
	low := strings.ToLower(out)
	if strings.Contains(low, "failed") || strings.Contains(low, "unable") || strings.Contains(low, "error") {
		return out, fmt.Errorf("%s", out)
	}
	return out, nil
}

func (c *Client) Disconnect(target string) (string, error) {
	args := []string{"disconnect"}
	if strings.TrimSpace(target) != "" {
		args = append(args, strings.TrimSpace(target))
	}
	res, err := c.RunTimeout("", 15*time.Second, args...)
	if err != nil {
		return res.Combined, err
	}
	return strings.TrimSpace(res.Combined), nil
}

func (c *Client) Pair(hostPort, code string) (string, error) {
	hostPort = strings.TrimSpace(hostPort)
	code = strings.TrimSpace(code)
	res, err := c.RunTimeout("", 30*time.Second, "pair", hostPort, code)
	out := strings.TrimSpace(res.Combined)
	if err != nil {
		return out, err
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "failed") || strings.Contains(low, "error") {
		return out, fmt.Errorf("%s", out)
	}
	return out, nil
}
