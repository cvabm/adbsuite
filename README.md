# ADB Suite

基于 **Go + Wails v2 + React/TypeScript** 的一站式 Android ADB 桌面工具。

内置目录放置 `adb` 与 `scrcpy` 后，**一个程序目录即可使用**（也可在设置中指定路径）。

## 功能

- **设备**：列表、信息、前台 Activity、批量勾选
- **投屏**：内置 scrcpy，单台/多台并行
- **应用**：安装/卸载/清数据/包列表，批量装包
- **文件**：Device Explorer（浏览/上传/下载/新建/重命名/删除，类似 Android Studio）
- **工具**：截图、录屏、shell、端口 forward/reverse、重启
- **日志**：实时 logcat、级别与关键字过滤

## 准备内置工具

见 [`bin/README.md`](bin/README.md)。

简要：

1. 将 Platform Tools 放入 `bin/platform-tools/`（含 `adb.exe`）
2. 将 scrcpy 放入 `bin/scrcpy/`（含 `scrcpy.exe`）

## 开发

环境：Go 1.25+、Node 18+、Wails v2、Windows 上需 WebView2。

```bash
cd adbsuite
wails dev
```

## 打包

```bash
wails build
```

产物在 `build/bin/adbsuite.exe`。把项目里的 `bin/` 整夹复制到 exe 同级：

```text
adbsuite.exe
bin/
  platform-tools/
  scrcpy/
```

## 配置

设置保存在用户配置目录：`%AppData%/adbsuite/settings.json`。

## License

按需使用。adb / scrcpy 请遵守各自官方许可。
