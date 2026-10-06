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

当前内置 [scrcpy 5.0](https://github.com/Genymobile/scrcpy/releases/tag/v5.0) 的官方 Windows 64 位包，默认自动使用硬件解码，不支持时回退到软件解码。

来源：[scrcpy-win64-v5.0.zip](https://github.com/Genymobile/scrcpy/releases/download/v5.0/scrcpy-win64-v5.0.zip)。官方 SHA-256：

```text
44c10d9e82f20ea67227d14d37bf9fbe3603117c5736df3f514544a02ba20a73
```

更新时完整替换下列目录，保留官方包中的 DLL、`scrcpy-server`、adb 和许可文件，避免混用不同版本：

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
