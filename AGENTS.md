# 仓库工作约定

## 发布与版本

- 用户说“更新一版”或“更新版本”，是要求完成 GitHub Release 发布，不只是本地构建或提交、push。
- 发布前先读取 `.github/workflows/release.yml`，并核对远端 workflow、已有 tag 和 Release；不要凭记忆猜测触发方式。
- 当前 Release workflow 在代码或 workflow 修改推送到 `main` 时自动运行；仅修改 Markdown、`.gitignore` 或 `LICENSE*` 不触发。普通代码 push 已触发发布时，不再额外手动触发，避免重复升版。
- workflow 自动从已有 `vMAJOR.MINOR.PATCH` tag 计算版本，默认递增 patch。不要在本地提前创建发布 tag，也不要把 `frontend/package.json` 的内部版本当成发行版本。
- 只有文档修改、需要重新发布或用户指定 minor/major 升版时，通过 `workflow_dispatch` 选择 bump；先确认没有重复发布正在运行。
- 发布产物由 GitHub Actions 构建并附到 Release。不手动上传本地 exe、ZIP，也不将构建产物、浏览器验证文件、缓存或用户配置提交到仓库。
- Windows ZIP 必须包含 `adbsuite.exe`、整个 `bin/`（adb/scrcpy、DLL、server、许可文件）及 README。当前工具没有嵌入 exe，不单独分发裸 exe；单 exe 内嵌方案需要另行明确要求。
- 更新完成前检查 Actions 最终状态、Release 的新 tag 和 ZIP 附件。若失败，检查日志并修复；不能把“已触发”当作“已发布”。不能覆盖或强推既有发行 tag。

## 构建与检查

- 当前已验证 Go 1.26.6、Node.js 24.18.0、Wails v2.13.0；构建 CLI 必须与 go.mod 的 Wails 版本匹配，依赖安装使用 `npm ci`。
- 前端检查：`cd frontend` 后运行 `npm test`、`npm run build`。
- Go 检查：`go test ./...`、`go vet ./...`；本机 GCC 可用时运行 `go test -race ./...`。
- 修改布局应验证长文本、窄窗口、浅色和深色。布局夹具不等于真实 Wails 窗口验收，不夸大验证范围。
- 连接设备的自动诊断默认只读；真实上传、删除、安装/卸载、清日志、录屏和投屏操作不能当作无副作用检查。

## 提交与范围

- 保留用户已有改动；源码、测试、接口绑定与有关文档一并提交，排除本地产物。
- 本地提交使用用户的全局 Git 作者配置，不覆盖仓库级作者。CI 创建发行 tag 可以使用 GitHub Actions bot 身份。
- 用户要求提交或发布时才提交、push；发布请求包含实现相应流程所需的提交、push 和 Actions 触发。
- 修复应用加载、进程生命周期或录屏逻辑时，保留设备/请求隔离、元数据不覆盖操作状态、按自身进程精确停止等安全保护。
