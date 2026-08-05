package adb

import (
	"fmt"
	"strings"
	"time"
)

type Device struct {
	Serial      string `json:"serial"`
	State       string `json:"state"`
	Model       string `json:"model"`
	Product     string `json:"product"`
	TransportID string `json:"transportId"`
	IsWireless  bool   `json:"isWireless"`
	USB         string `json:"usb"`
}

func (c *Client) ListDevices() ([]Device, error) {
	res, err := c.RunTimeout("", 10*time.Second, "devices", "-l")
	if err != nil {
		return nil, err
	}
	var devices []Device
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{
			Serial: fields[0],
			State:  fields[1],
		}
		for _, f := range fields[2:] {
			if k, v, ok := strings.Cut(f, ":"); ok {
				switch k {
				case "model":
					d.Model = v
				case "product":
					d.Product = v
				case "transport_id":
					d.TransportID = v
				case "usb":
					d.USB = v
				}
			}
		}
		d.IsWireless = strings.Contains(d.Serial, ":") || strings.HasPrefix(d.Serial, "emulator-")
		if strings.Contains(d.Serial, ":") {
			d.IsWireless = true
		}
		// USB field present => wired; serial with host:port => wireless
		if d.USB != "" {
			d.IsWireless = false
		}
		if strings.Contains(d.Serial, ":") {
			d.IsWireless = true
		}
		devices = append(devices, d)
	}
	if devices == nil {
		devices = []Device{}
	}
	return devices, nil
}

type DeviceInfo map[string]string

func (c *Client) DeviceInfo(serial string) (DeviceInfo, error) {
	props := []struct {
		key, label string
	}{
		{"ro.product.brand", "品牌"},
		{"ro.product.model", "型号"},
		{"ro.product.manufacturer", "厂商"},
		{"ro.build.version.release", "Android 版本"},
		{"ro.build.version.sdk", "SDK"},
		{"ro.serialno", "序列号"},
		{"ro.product.board", "处理器/板型"},
		{"ro.product.cpu.abi", "CPU ABI"},
	}
	info := DeviceInfo{}
	for _, p := range props {
		res, err := c.Shell(serial, "getprop "+p.key)
		if err == nil {
			v := strings.TrimSpace(res.Stdout)
			if v != "" {
				info[p.label] = v
			}
		}
	}
	if res, err := c.Shell(serial, "wm size"); err == nil {
		info["分辨率"] = parsePrefixed(res.Stdout, "Physical size:")
	}
	if res, err := c.Shell(serial, "wm density"); err == nil {
		info["屏幕密度"] = parsePrefixed(res.Stdout, "Physical density:")
	}
	if res, err := c.Shell(serial, "dumpsys battery | grep level"); err == nil {
		info["电量"] = strings.TrimSpace(strings.ReplaceAll(res.Stdout, "level:", ""))
	}
	if res, err := c.Shell(serial, "ls /sys/class/usb_host 2>/dev/null"); err == nil {
		if strings.TrimSpace(res.Stdout) != "" {
			info["支持 OTG"] = "是"
		} else {
			info["支持 OTG"] = "否"
		}
	}
	info["序列号(连接)"] = serial
	if len(info) == 0 {
		return nil, fmt.Errorf("无法读取设备信息")
	}
	return info, nil
}

func parsePrefixed(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, prefix) {
			return strings.TrimSpace(strings.SplitN(line, prefix, 2)[1])
		}
	}
	return strings.TrimSpace(out)
}

func (c *Client) Reboot(serial, mode string) error {
	switch mode {
	case "bootloader", "recovery":
		_, err := c.RunTimeout(serial, 30*time.Second, "reboot", mode)
		return err
	default:
		_, err := c.RunTimeout(serial, 30*time.Second, "reboot")
		return err
	}
}

func (c *Client) ForegroundActivity(serial string) (string, error) {
	// try modern dumpsys
	res, err := c.Shell(serial, "dumpsys activity activities | grep -E 'mResumedActivity|topResumedActivity' | head -n 3")
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		res2, err2 := c.Shell(serial, "dumpsys window | grep -E 'mCurrentFocus|mFocusedApp' | head -n 3")
		if err2 != nil {
			return "", err2
		}
		out = strings.TrimSpace(res2.Stdout)
	}
	if out == "" {
		return "(未获取到前台 Activity)", nil
	}
	return out, nil
}
