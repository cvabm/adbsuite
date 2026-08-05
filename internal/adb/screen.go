package adb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (c *Client) Screenshot(serial, localPath string) (string, error) {
	if localPath == "" {
		localPath = filepath.Join(os.TempDir(), fmt.Sprintf("adbsuite_%d.png", time.Now().Unix()))
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil && !os.IsExist(err) {
		// if only filename, Dir may be "."
		_ = err
	}
	remote := "/sdcard/adbsuite_shot.png"
	if _, err := c.Shell(serial, "screencap -p "+remote); err != nil {
		return "", err
	}
	if _, err := c.Pull(serial, remote, localPath); err != nil {
		return "", err
	}
	_, _ = c.Shell(serial, "rm -f "+remote)
	return localPath, nil
}

func (c *Client) Forward(serial, local, remote string) (string, error) {
	res, err := c.RunTimeout(serial, 15*time.Second, "forward", local, remote)
	if err != nil {
		return res.Combined, err
	}
	return firstNonEmpty(strings.TrimSpace(res.Combined), "OK"), nil
}

func (c *Client) Reverse(serial, remote, local string) (string, error) {
	res, err := c.RunTimeout(serial, 15*time.Second, "reverse", remote, local)
	if err != nil {
		return res.Combined, err
	}
	return firstNonEmpty(strings.TrimSpace(res.Combined), "OK"), nil
}

func (c *Client) ForwardList(serial string) (string, error) {
	res, err := c.RunTimeout(serial, 10*time.Second, "forward", "--list")
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func (c *Client) ReverseList(serial string) (string, error) {
	res, err := c.RunTimeout(serial, 10*time.Second, "reverse", "--list")
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func (c *Client) ForwardRemoveAll(serial string) error {
	_, err := c.RunTimeout(serial, 10*time.Second, "forward", "--remove-all")
	return err
}

func (c *Client) ReverseRemoveAll(serial string) error {
	_, err := c.RunTimeout(serial, 10*time.Second, "reverse", "--remove-all")
	return err
}
