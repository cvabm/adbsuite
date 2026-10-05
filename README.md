# ADB Suite

基于 **Go + Wails v2 + React/TypeScript** 的 Android ADB 桌面工具。

仓库已内置 `bin/platform-tools`（adb）与 `bin/scrcpy`，**一个程序目录即可使用**，无需系统 PATH，也无需在设置里配置工具路径。

## 功能

| 模块 | 说明 |
|------|------|
| **设备** | 列表刷新、勾选、设备信息、无线连接相关操作 |
| **投屏** | 内置 scrcpy，单台 / 多台并行，参数可在设置中配置 |
| **应用** | 普通/系统应用切换、搜索、安装/卸载/清数据，批量装包 |
| **服务** | 运行中 Service 列表（`dumpsys activity services`）、普通/系统/前台筛选、停止服务 / 强制停止应用 |
| **文件** | Device Explorer：浏览、上传、下载、新建、重命名、删除（类似 Android Studio） |
| **工具** | 截图、录屏、前台 Activity、Shell、端口 Forward / Reverse |
| **日志** | 实时 logcat；V/D/I/W/E/A 等级勾选与色块；关键字过滤；暂停 / 滚底 |
| **设置** | 主题（浅/深）、批量并发、scrcpy 默认参数 |

应用刷新先显示包列表和已缓存的应用名，再后台分批补齐未缓存名称、并行读取版本；首次无需等所有 APK 资源解析完成。补齐过程中仍可搜索和操作应用；名称未解析时暂显示包名末段，按应用名/版本搜索的结果会随信息补齐更新。切换设备或再次刷新后旧结果不会覆盖新列表；信息读取失败不影响已有列表。

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

已验证环境：Go 1.26.6、Node.js 24.18.0、[Wails v2.13.0](https://wails.io/)，Windows 需 WebView2。项目锁定 Go 1.26.6 工具链；自动工具链启用时会按需下载。

```bash
git clone https://github.com/cvabm/adbsuite.git
cd adbsuite
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev
```

## 打包

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build -trimpath -platform windows/amd64
```

产物：`build/bin/adbsuite.exe`。把项目里的 **`bin/` 整夹** 复制到 exe 同级后再分发：

```text
adbsuite.exe
bin/
  platform-tools/
  scrcpy/
```

应用图标：`build/appicon.png` / `build/windows/icon.ico`（可用 `build/gen_icon.py` 重新生成）。

## 发布

由 [GitHub Actions](https://github.com/cvabm/adbsuite/actions/workflows/release.yml) 自动构建和发布 Windows ZIP，内含 exe、完整 `bin/` 与 README；不需要手动上传本地产物。

推送代码到 `main` 默认自动递增一个 patch 版本并创建 Release。仅改 Markdown、`.gitignore` 或 `LICENSE*` 不触发发布；需要发布时可手动运行 Release workflow，选择 patch/minor/major。版本以 Actions 创建的 `vMAJOR.MINOR.PATCH` tag 为准，不要提前手动创建 tag。以 workflow 成功且 Release ZIP 附件就绪为发布完成。

## 配置

设置保存在用户配置目录：

- Windows：`%AppData%\adbsuite\settings.json`

## 测试与安全保护

```powershell
cd frontend
npm ci
npm test
npm run build
cd ..
go test -race -count=3 ./...
go vet ./...
```

竞态检查需要 PATH 中可用的 GCC。前端请求隔离测试使用 Node.js 的 TypeScript 类型擦除能力，建议使用上述已验证的 Node 版本。

- 切换设备会重建文件视图，旧目录请求不能覆盖当前结果；文件操作执行中禁止手动切换设备。
- 投屏与日志按实际进程身份清理；日志事件另带会话标识，旧事件不能改变新会话状态。
- 停止录屏仅针对本次 PID，且验证 `screenrecord` 可执行文件名和精确输出路径；不执行全局停止录屏，也不回收其他录屏文件。无法确认停止时保留会话供重试；设备原有的 180 秒录屏上限不变。
- 投屏异常退出及端口操作失败会显示错误，而不是无条件提示成功。

已有设备的只读验证可选择运行：

```powershell
$env:ADBSUITE_TEST_ADB = (Resolve-Path .\bin\platform-tools\adb.exe).Path
$env:ADBSUITE_TEST_SERIAL = '你的设备序列号'
go test -race -run '^TestConnectedDeviceReadOnly$' -v ./internal/adb
Remove-Item Env:ADBSUITE_TEST_ADB, Env:ADBSUITE_TEST_SERIAL
```

该检查只读取设备信息、目录、端口列表和日志，不清空 logcat、不写入或删除手机文件、不启动或停止手机录屏。模拟进程回归测试不需要连接手机；图形界面及真实录屏流程仍需人工验收。

## License

本仓库代码按需使用。内置的 **adb**（Android Platform Tools）与 **scrcpy** 请遵守各自官方许可。
