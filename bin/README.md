# 内置工具目录

将官方二进制放到对应位置后，ADB Suite 会优先使用这里的工具（无需系统 PATH）。

## platform-tools（adb）

从 [Android Platform Tools](https://developer.android.com/tools/releases/platform-tools) 下载解压，把文件放进本目录：

```text
bin/platform-tools/
  adb.exe          # Windows 必需
  AdbWinApi.dll
  AdbWinUsbApi.dll
  ...
```

## scrcpy

从 [scrcpy Releases](https://github.com/Genymobile/scrcpy/releases) 下载 Windows 包，解压到：

```text
bin/scrcpy/
  scrcpy.exe
  scrcpy-server
  *.dll
  ...
```

## 打包分发

`wails build` 后，把整个 `bin/` 目录复制到 `adbsuite.exe` 同级即可「一个程序目录搞定」。

开发时 `wails dev` 会从项目根目录查找 `bin/`。
