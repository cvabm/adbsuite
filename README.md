# ADB Suite

基于 **Go + Wails v2 + React/TypeScript** 的 Android ADB 桌面工具。

仓库已内置 `bin/platform-tools`（adb）与 `bin/scrcpy`，**一个程序目录即可使用**，无需系统 PATH，也无需在设置里配置工具路径。

## 功能

| 模块 | 说明 |
|------|------|
| **设备** | 列表刷新、勾选、设备信息、无线连接相关操作 |
| **投屏** | 内置 scrcpy，单台 / 多台并行，参数可在设置中配置 |
| **应用** | 普通/系统应用切换、搜索、安装/卸载/清数据，批量装包 |
| **文件** | Device Explorer：浏览、上传、下载、新建、重命名、删除（类似 Android Studio） |
| **工具** | 截图、录屏、前台 Activity、Shell、端口 Forward / Reverse |
| **日志** | 实时 logcat；V/D/I/W/E/A 等级勾选与色块；关键字过滤；暂停 / 滚底 |
| **设置** | 主题（浅/深）、批量并发、scrcpy 默认参数 |

## 目录结构（运行时）

```text
adbsuite.exe          # 或开发时项目根目录
bin/
  platform-tools/     # adb.exe 及依赖
  scrcpy/             # scrcpy.exe、scrcpy-server 及依赖
```

路径解析规则：优先内置 `bin/`，找不到时再回退到 PATH 中的同名命令。

更细的内置工具说明见 [`bin/README.md`](bin/README.md)。

## 开发

环境：Go 1.25+、Node 18+、[Wails v2](https://wails.io/)、Windows 需 WebView2。

```bash
git clone https://github.com/cvabm/adbsuite.git
cd adbsuite
wails dev
```

## 打包

```bash
wails build
```

产物：`build/bin/adbsuite.exe`。把项目里的 **`bin/` 整夹** 复制到 exe 同级后再分发：

```text
adbsuite.exe
bin/
  platform-tools/
  scrcpy/
```

应用图标：`build/appicon.png` / `build/windows/icon.ico`（可用 `build/gen_icon.py` 重新生成）。

## 配置

设置保存在用户配置目录：

- Windows：`%AppData%\adbsuite\settings.json`

## License

本仓库代码按需使用。内置的 **adb**（Android Platform Tools）与 **scrcpy** 请遵守各自官方许可。
