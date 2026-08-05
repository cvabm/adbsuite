import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn } from "../wailsjs/runtime/runtime";
import {
  Bootstrap,
  ListDevices,
  DeviceInfo,
  SaveSettings,
  GetToolPaths,
  InstallApk,
  UninstallPackage,
  ListPackages,
  ClearPackage,
  Screenshot,
  StartScreenRecord,
  StopScreenRecord,
  Shell,
  Forward,
  Reverse,
  PortList,
  PortRemoveAll,
  ForegroundActivity,
  StartScrcpy,
  StopScrcpy,
  ListScrcpy,
  StartLogcat,
  StopLogcat,
  BatchInstall,
  BatchScreenshot,
  BatchStartScrcpy,
  BatchStopScrcpy,
  SelectFile,
  SelectSaveFile,
  SelectDirectory,
  TestAdb,
} from "../wailsjs/go/main/App";
import DeviceExplorer from "./components/DeviceExplorer";
import "./App.css";

type Tab =
  | "devices"
  | "mirror"
  | "apps"
  | "files"
  | "tools"
  | "logcat"
  | "settings";

type Device = {
  serial: string;
  state: string;
  model: string;
  product: string;
  isWireless: boolean;
  usb: string;
};

type Pkg = { name: string; label?: string; path?: string; system?: boolean };
type AppKindFilter = "third" | "system" | "all";
type Settings = {
  adbPath: string;
  scrcpyPath: string;
  theme: string;
  maxConcurrency: number;
  scrcpyMaxSize: number;
  scrcpyBitRate: string;
  scrcpyMaxFps: number;
  scrcpyStayAwake: boolean;
  scrcpyNoAudio: boolean;
  killScrcpyOnExit: boolean;
  recentHosts: string[];
  lastDevice: string;
  tcpipPort: number;
};

const TABS: { id: Tab; label: string }[] = [
  { id: "devices", label: "设备" },
  { id: "mirror", label: "投屏" },
  { id: "apps", label: "应用" },
  { id: "files", label: "文件" },
  { id: "tools", label: "工具" },
  { id: "logcat", label: "日志" },
  { id: "settings", label: "设置" },
];

const LOG_LEVELS = ["V", "D", "I", "W", "E", "A"] as const;

function parseLevel(line: string): string {
  const m = line.match(/\s([VDIWEFA])\//);
  if (!m) return "?";
  // logcat 里 Fatal 也按 Assert 色/筛选处理
  return m[1] === "F" ? "A" : m[1];
}

export default function App() {
  const [booted, setBooted] = useState(false);
  const [tab, setTab] = useState<Tab>("devices");
  const [devices, setDevices] = useState<Device[]>([]);
  const [selected, setSelected] = useState("");
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [info, setInfo] = useState<Record<string, string> | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [paths, setPaths] = useState<{ adb: string; scrcpy: string } | null>(null);
  const [packages, setPackages] = useState<Pkg[]>([]);
  const [pkgFilter, setPkgFilter] = useState("");
  const [appKind, setAppKind] = useState<AppKindFilter>("third");
  const [scrcpySessions, setScrcpySessions] = useState<{ serial: string; pid: number }[]>([]);
  const [logLines, setLogLines] = useState<string[]>([]);
  const [logRunning, setLogRunning] = useState(false);
  const [logPaused, setLogPaused] = useState(false);
  const [logFilter, setLogFilter] = useState("");
  const [logLevels, setLogLevels] = useState<Record<string, boolean>>({
    V: true,
    D: true,
    I: true,
    W: true,
    E: true,
    A: true,
  });
  const logBuffer = useRef<string[]>([]);
  const logEndRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  // form fields
  const [installOpts, setInstallOpts] = useState({ r: true, d: false, g: false });
  const [shellCmd, setShellCmd] = useState("");
  const [shellOut, setShellOut] = useState("");
  const [fwdLocal, setFwdLocal] = useState("tcp:8080");
  const [fwdRemote, setFwdRemote] = useState("tcp:8080");
  const [portOut, setPortOut] = useState("");
  const [fgActivity, setFgActivity] = useState("");
  const [recording, setRecording] = useState(false);
  const [recordLocal, setRecordLocal] = useState("");
  const [pkgName, setPkgName] = useState("");
  const devicesSig = useRef("");
  const logPausedRef = useRef(false);

  // Lightweight status for components that still expect a log callback (no bottom panel).
  const log = useCallback((_msg: string) => {
    /* intentionally no global output panel */
  }, []);

  const run = useCallback(
    async <T,>(label: string, fn: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true);
      try {
        return await fn();
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        window.alert(`${label}失败：${msg}`);
        return undefined;
      } finally {
        setBusy(false);
      }
    },
    []
  );

  const applyDevices = useCallback((list: Device[]) => {
    const next = list || [];
    const sig = next.map((d) => `${d.serial}|${d.state}|${d.model}`).join(";");
    if (sig !== devicesSig.current) {
      devicesSig.current = sig;
      setDevices(next);
    }
    setSelected((cur) => {
      if (next.length === 0) return "";
      if (cur && next.some((d) => d.serial === cur)) return cur;
      return next[0].serial;
    });
  }, []);

  const refreshDevices = useCallback(async (silent = false) => {
    try {
      const list = ((await ListDevices()) as Device[]) || [];
      applyDevices(list);
    } catch (e: unknown) {
      if (!silent) {
        window.alert(`刷新设备失败：${e instanceof Error ? e.message : String(e)}`);
      }
    }
  }, [applyDevices]);

  const refreshScrcpy = useCallback(async () => {
    try {
      const list = ((await ListScrcpy()) as { serial: string; pid: number }[]) || [];
      setScrcpySessions((prev) => {
        const a = prev.map((x) => x.serial + ":" + x.pid).join(",");
        const b = list.map((x) => x.serial + ":" + x.pid).join(",");
        return a === b ? prev : list;
      });
    } catch {
      /* ignore */
    }
  }, []);

  useEffect(() => {
    logPausedRef.current = logPaused;
  }, [logPaused]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // 一次 Bootstrap，前端只做一次状态提交 → 界面只「出现」一次
        const boot = (await Bootstrap()) as unknown as {
          settings: Settings;
          paths: { adb: string; scrcpy: string };
          devices: Device[];
          scrcpy: { serial: string; pid: number }[];
          adbOk: boolean;
          adbMsg: string;
        };
        if (cancelled) return;
        const list = boot.devices || [];
        devicesSig.current = list.map((d) => `${d.serial}|${d.state}|${d.model}`).join(";");
        const first = list[0]?.serial || "";
        if (!boot.adbOk) {
          window.alert(`adb 不可用: ${boot.adbMsg || ""}（请检查 bin/platform-tools）`);
        }
        setSettings(boot.settings);
        setPaths(boot.paths);
        setDevices(list);
        setSelected(first);
        setScrcpySessions(boot.scrcpy || []);
        setBooted(true);
      } catch (e: unknown) {
        if (!cancelled) {
          window.alert(
            `初始化失败: ${e instanceof Error ? e.message : String(e)}`
          );
          setBooted(true);
        }
      }
    })();

    const off1 = EventsOn("logcat:line", (line: string) => {
      if (logPausedRef.current) {
        logBuffer.current.push(line);
        return;
      }
      setLogLines((prev) => {
        const next = prev.length > 50000 ? prev.slice(-40000) : prev.slice();
        next.push(line);
        return next;
      });
    });
    const off2 = EventsOn("logcat:stopped", () => {
      setLogRunning(false);
    });

    return () => {
      cancelled = true;
      off1();
      off2();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (autoScroll && tab === "logcat") {
      logEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [logLines, autoScroll, tab]);

  const checkedSerials = useMemo(
    () => Object.entries(checked).filter(([, v]) => v).map(([k]) => k),
    [checked]
  );

  const filteredPkgs = useMemo(() => {
    const q = pkgFilter.trim().toLowerCase();
    return packages.filter((p) => {
      if (appKind === "third" && p.system) return false;
      if (appKind === "system" && !p.system) return false;
      if (!q) return true;
      const label = (p.label || "").toLowerCase();
      return label.includes(q);
    });
  }, [packages, pkgFilter, appKind]);

  const selectedPkg = useMemo(
    () => packages.find((p) => p.name === pkgName) || null,
    [packages, pkgName]
  );

  const appDisplayName = (p: Pkg) => (p.label && p.label.trim()) || p.name;

  const appCounts = useMemo(() => {
    let third = 0;
    let system = 0;
    for (const p of packages) {
      if (p.system) system++;
      else third++;
    }
    return { third, system, all: packages.length };
  }, [packages]);

  const filteredLogs = useMemo(() => {
    const q = logFilter.trim().toLowerCase();
    return logLines.filter((line) => {
      const lv = parseLevel(line);
      if (lv !== "?" && !logLevels[lv]) return false;
      if (q && !line.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [logLines, logFilter, logLevels]);

  const needDevice = () => {
    if (!selected) {
      window.alert("请先选择设备");
      return false;
    }
    return true;
  };

  const themeClass = settings?.theme === "dark" ? "theme-dark" : "theme-light";

  if (!booted) {
    return (
      <div className="boot-screen theme-light">
        <div className="boot-text">正在启动…</div>
      </div>
    );
  }

  return (
    <div className={`app ${themeClass}`}>
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-title">ADB Suite</div>
          <div className="brand-sub">Go · Wails · 一站式</div>
        </div>
        <nav>
          {TABS.map((t) => (
            <button
              key={t.id}
              className={tab === t.id ? "nav active" : "nav"}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </nav>
        <div className="sidebar-foot">
          <button className="btn ghost" disabled={busy} onClick={() => refreshDevices()}>
            刷新设备
          </button>
          <div className="device-count">{devices.length} 台设备</div>
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div className="device-picker">
            <label>当前设备</label>
            <select value={selected} onChange={(e) => setSelected(e.target.value)}>
              <option value="">— 无 —</option>
              {devices.map((d) => (
                <option key={d.serial} value={d.serial}>
                  {d.serial} [{d.state}] {d.model || ""}
                </option>
              ))}
            </select>
          </div>
          {busy && <span className="badge busy">工作中…</span>}
        </header>

        <div className="content">
          {tab === "devices" && (
            <section className="panel">
              <h2>设备列表</h2>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>批量</th>
                      <th>序列号</th>
                      <th>状态</th>
                      <th>型号</th>
                      <th>连接</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {devices.length === 0 && (
                      <tr>
                        <td colSpan={6} className="muted">
                          未检测到设备。请用 USB 连接手机并开启 USB 调试。
                        </td>
                      </tr>
                    )}
                    {devices.map((d) => (
                      <tr key={d.serial} className={selected === d.serial ? "row-active" : ""}>
                        <td>
                          <input
                            type="checkbox"
                            checked={!!checked[d.serial]}
                            onChange={(e) =>
                              setChecked((c) => ({ ...c, [d.serial]: e.target.checked }))
                            }
                          />
                        </td>
                        <td className="mono">{d.serial}</td>
                        <td>
                          <span className={`st st-${d.state}`}>{d.state}</span>
                        </td>
                        <td>{d.model || d.product || "—"}</td>
                        <td>{d.isWireless ? "无线" : "USB"}</td>
                        <td>
                          <button className="btn sm" onClick={() => setSelected(d.serial)}>
                            选用
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="row gap">
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("设备信息", async () => {
                      const i = await DeviceInfo(selected);
                      setInfo(i as Record<string, string>);
                    })
                  }
                >
                  设备信息
                </button>
              </div>

              {info && (
                <div className="info-grid">
                  {Object.entries(info).map(([k, v]) => (
                    <div key={k} className="info-item">
                      <span className="k">{k}</span>
                      <span className="v mono">{v}</span>
                    </div>
                  ))}
                </div>
              )}
            </section>
          )}

          {tab === "mirror" && (
            <section className="panel">
              <h2>投屏（scrcpy）</h2>
              <p className="hint">使用内置 bin/scrcpy，参数在「设置」中配置。</p>
              <div className="row gap wrap">
                <button
                  className="btn primary"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("启动投屏", async () => {
                      await StartScrcpy(selected);
                      await refreshScrcpy();
                    })
                  }
                >
                  投屏当前设备
                </button>
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("停止投屏", async () => {
                      await StopScrcpy(selected);
                      await refreshScrcpy();
                    })
                  }
                >
                  停止当前
                </button>
                <button
                  className="btn"
                  disabled={busy || checkedSerials.length === 0}
                  onClick={() =>
                    run("批量投屏", async () => {
                      await BatchStartScrcpy(checkedSerials);
                      await refreshScrcpy();
                    })
                  }
                >
                  批量投屏已选 ({checkedSerials.length})
                </button>
                <button
                  className="btn"
                  disabled={busy || checkedSerials.length === 0}
                  onClick={() =>
                    run("批量停投屏", async () => {
                      await BatchStopScrcpy(checkedSerials);
                      await refreshScrcpy();
                    })
                  }
                >
                  批量停止已选
                </button>
              </div>
              <h3>会话</h3>
              <ul className="list">
                {scrcpySessions.length === 0 && <li className="muted">无活动投屏</li>}
                {scrcpySessions.map((s) => (
                  <li key={s.serial}>
                    <span className="mono">{s.serial}</span> · PID {s.pid}
                    <button
                      className="btn sm"
                      onClick={() =>
                        run("停止", async () => {
                          await StopScrcpy(s.serial);
                          await refreshScrcpy();
                        })
                      }
                    >
                      停止
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {tab === "apps" && (
            <section className="panel apps-panel">
              <h2>应用</h2>
              <p className="hint">
                只显示应用名；普通应用与系统应用分开管理。点选后可卸载或清除数据。系统应用卸载为「当前用户卸载」。首次刷新会读取应用名并缓存。
              </p>
              <div className="row gap wrap">
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={installOpts.r}
                    onChange={(e) => setInstallOpts((o) => ({ ...o, r: e.target.checked }))}
                  />
                  重装 -r
                </label>
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={installOpts.d}
                    onChange={(e) => setInstallOpts((o) => ({ ...o, d: e.target.checked }))}
                  />
                  降级 -d
                </label>
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={installOpts.g}
                    onChange={(e) => setInstallOpts((o) => ({ ...o, g: e.target.checked }))}
                  />
                  授权 -g
                </label>
                <button
                  className="btn primary"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("安装", async () => {
                      if (!needDevice()) return;
                      const f = await SelectFile("选择 APK", ["*.apk"]);
                      if (!f) return;
                      await InstallApk(
                        selected,
                        f,
                        installOpts.r,
                        installOpts.d,
                        installOpts.g
                      );
                    })
                  }
                >
                  安装 APK
                </button>
                <button
                  className="btn"
                  disabled={busy || checkedSerials.length === 0}
                  onClick={() =>
                    run("批量安装", async () => {
                      const f = await SelectFile("选择 APK（批量）", ["*.apk"]);
                      if (!f) return;
                      await BatchInstall(
                        checkedSerials,
                        f,
                        installOpts.r,
                        installOpts.d,
                        installOpts.g
                      );
                    })
                  }
                >
                  批量安装到已选
                </button>
              </div>
              <div className="row gap wrap">
                <div className="seg" role="tablist" aria-label="应用类型">
                  {(
                    [
                      { id: "third" as const, label: "普通应用", count: appCounts.third },
                      { id: "system" as const, label: "系统应用", count: appCounts.system },
                      { id: "all" as const, label: "全部", count: appCounts.all },
                    ] as const
                  ).map((opt) => (
                    <button
                      key={opt.id}
                      type="button"
                      className={"seg-btn" + (appKind === opt.id ? " active" : "")}
                      onClick={() => setAppKind(opt.id)}
                    >
                      {opt.label}
                      {packages.length > 0 ? (
                        <span className="seg-count">{opt.count}</span>
                      ) : null}
                    </button>
                  ))}
                </div>
                <input
                  placeholder="搜索应用名"
                  value={pkgFilter}
                  onChange={(e) => setPkgFilter(e.target.value)}
                  style={{ flex: 1, minWidth: 160 }}
                />
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("刷新应用", async () => {
                      if (!needDevice()) return;
                      const list = (await ListPackages(selected, "all")) as Pkg[];
                      setPackages(list || []);
                      if (pkgName && !(list || []).some((p) => p.name === pkgName)) {
                        setPkgName("");
                      }
                    })
                  }
                >
                  刷新列表
                </button>
              </div>
              <div className="row gap wrap apps-action-bar">
                <div className="apps-selected muted">
                  {selectedPkg ? (
                    <>
                      已选：
                      <strong className="text-strong">{appDisplayName(selectedPkg)}</strong>
                      <span
                        className={"app-badge " + (selectedPkg.system ? "system" : "user")}
                      >
                        {selectedPkg.system ? "系统" : "普通"}
                      </span>
                    </>
                  ) : (
                    "点击下方应用名进行选择"
                  )}
                </div>
                <button
                  className="btn danger"
                  disabled={busy || !selected || !pkgName}
                  onClick={() =>
                    run("卸载", async () => {
                      if (!needDevice() || !selectedPkg) return;
                      const name = appDisplayName(selectedPkg);
                      const isSys = !!selectedPkg.system;
                      const tip = isSys
                        ? `确定卸载系统应用「${name}」？\n将为当前用户卸载（非 root 无法从系统分区删除）。`
                        : `确定卸载「${name}」？`;
                      if (!window.confirm(tip)) return;
                      await UninstallPackage(
                        selected,
                        selectedPkg.name,
                        false,
                        isSys
                      );
                      setPackages((prev) => prev.filter((p) => p.name !== selectedPkg.name));
                      setPkgName("");
                    })
                  }
                >
                  {selectedPkg?.system ? "卸载（当前用户）" : "卸载"}
                </button>
                <button
                  className="btn"
                  disabled={busy || !selected || !pkgName}
                  onClick={() =>
                    run("清除数据", async () => {
                      if (!needDevice() || !selectedPkg) return;
                      const name = appDisplayName(selectedPkg);
                      const kind = selectedPkg.system ? "系统应用" : "应用";
                      if (!window.confirm(`确定清除${kind}「${name}」的数据？`)) return;
                      await ClearPackage(selected, selectedPkg.name);
                    })
                  }
                >
                  清除数据
                </button>
              </div>
              <div className="table-wrap tall apps-list">
                <table>
                  <thead>
                    <tr>
                      <th>应用名</th>
                      <th style={{ width: 72 }}>类型</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredPkgs.length === 0 && (
                      <tr>
                        <td colSpan={2} className="muted">
                          {packages.length === 0 ? "点「刷新列表」加载应用" : "无匹配应用"}
                        </td>
                      </tr>
                    )}
                    {filteredPkgs.slice(0, 1200).map((p) => (
                      <tr
                        key={p.name}
                        className={
                          (pkgName === p.name ? "row-active " : "") +
                          (p.system ? "app-row-system" : "app-row-user")
                        }
                        style={{ cursor: "pointer" }}
                        onClick={() => setPkgName(p.name)}
                      >
                        <td>{appDisplayName(p)}</td>
                        <td>
                          <span className={"app-badge " + (p.system ? "system" : "user")}>
                            {p.system ? "系统" : "普通"}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}

          {tab === "files" && (
            <DeviceExplorer
              serial={selected}
              busy={busy}
              setBusy={setBusy}
              log={log}
            />
          )}

          {tab === "tools" && (
            <section className="panel">
              <h2>工具</h2>
              <div className="row gap wrap">
                <button
                  className="btn primary"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("截图", async () => {
                      const f = await SelectSaveFile("保存截图", "screenshot.png");
                      if (!f) return;
                      await Screenshot(selected, f);
                    })
                  }
                >
                  截图
                </button>
                <button
                  className="btn"
                  disabled={busy || checkedSerials.length === 0}
                  onClick={() =>
                    run("批量截图", async () => {
                      const dir = await SelectDirectory("截图保存目录");
                      if (!dir) return;
                      await BatchScreenshot(checkedSerials, dir);
                    })
                  }
                >
                  批量截图已选
                </button>
                <button
                  className="btn"
                  disabled={busy || !selected || recording}
                  onClick={() =>
                    run("开始录屏", async () => {
                      const f = await SelectSaveFile("录屏保存为", "record.mp4");
                      if (!f) return;
                      await StartScreenRecord(selected, f);
                      setRecordLocal(f);
                      setRecording(true);
                    })
                  }
                >
                  开始录屏
                </button>
                <button
                  className="btn danger"
                  disabled={busy || !selected || !recording}
                  onClick={() =>
                    run("停止录屏", async () => {
                      const p = await StopScreenRecord(selected);
                      setRecording(false);
                      setRecordLocal("");
                      return p;
                    })
                  }
                >
                  停止录屏
                </button>
                {recording && (
                  <span className="badge busy">录屏中… {recordLocal ? `(${recordLocal})` : ""}</span>
                )}
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("前台 Activity", async () => {
                      const a = await ForegroundActivity(selected);
                      setFgActivity(a || "(空)");
                    })
                  }
                >
                  前台 Activity
                </button>
              </div>
              {fgActivity && (
                <div className="info-item" style={{ marginTop: 10 }}>
                  <span className="k">前台 Activity</span>
                  <span className="v mono">{fgActivity}</span>
                </div>
              )}

              <h3>Shell</h3>
              <div className="row gap">
                <input
                  style={{ flex: 1 }}
                  className="mono"
                  placeholder="例如 getprop ro.build.version.release"
                  value={shellCmd}
                  onChange={(e) => setShellCmd(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && shellCmd && selected) {
                      run("shell", async () => {
                        const o = await Shell(selected, shellCmd);
                        setShellOut(o || "(无输出)");
                      });
                    }
                  }}
                />
                <button
                  className="btn primary"
                  disabled={busy || !selected || !shellCmd}
                  onClick={() =>
                    run("shell", async () => {
                      const o = await Shell(selected, shellCmd);
                      setShellOut(o || "(无输出)");
                    })
                  }
                >
                  执行
                </button>
              </div>
              {shellOut && <pre className="tools-result mono">{shellOut}</pre>}

              <h3>端口转发</h3>
              <p className="hint">
                <strong>Forward</strong>：把电脑上的端口映射到手机。例如电脑访问{" "}
                <span className="mono">localhost:8080</span>，实际连到手机的 8080（调试手机上的服务时用）。
                <br />
                <strong>Reverse</strong>：方向相反，把手机上的端口映射到电脑。例如手机 App 访问{" "}
                <span className="mono">localhost:8080</span>，实际连到电脑的 8080（手机调试电脑上的后端时用）。
                <br />
                填写格式一般为 <span className="mono">tcp:端口号</span>，左侧为本机、右侧为设备侧。
              </p>
              <div className="row gap wrap">
                <label className="port-field">
                  <span className="muted">本机 (PC)</span>
                  <input value={fwdLocal} onChange={(e) => setFwdLocal(e.target.value)} />
                </label>
                <span className="muted">↔</span>
                <label className="port-field">
                  <span className="muted">设备</span>
                  <input value={fwdRemote} onChange={(e) => setFwdRemote(e.target.value)} />
                </label>
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("forward", async () => {
                      const o = await Forward(selected, fwdLocal, fwdRemote);
                      setPortOut(`Forward 完成\n${o || ""}`.trim());
                    })
                  }
                >
                  Forward
                </button>
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("reverse", async () => {
                      const o = await Reverse(selected, fwdRemote, fwdLocal);
                      setPortOut(`Reverse 完成\n${o || ""}`.trim());
                    })
                  }
                >
                  Reverse
                </button>
                <button
                  className="btn"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("端口列表", async () => {
                      const m = await PortList(selected);
                      setPortOut(
                        `Forward:\n${m.forward || "(空)"}\n\nReverse:\n${m.reverse || "(空)"}`
                      );
                    })
                  }
                >
                  列表
                </button>
                <button
                  className="btn danger"
                  disabled={busy || !selected}
                  onClick={() =>
                    run("清除端口", async () => {
                      await PortRemoveAll(selected);
                      setPortOut("已清除全部端口转发");
                    })
                  }
                >
                  全部清除
                </button>
              </div>
              {portOut && <pre className="tools-result mono">{portOut}</pre>}
            </section>
          )}

          {tab === "logcat" && (
            <section className="panel logcat-panel">
              <h2>Logcat</h2>
              <div className="row gap wrap">
                <button
                  className="btn primary"
                  disabled={busy || logRunning || !selected}
                  onClick={() =>
                    run("开始 logcat", async () => {
                      await StartLogcat(selected, true);
                      setLogLines([]);
                      setLogRunning(true);
                    })
                  }
                >
                  开始
                </button>
                <button
                  className="btn"
                  disabled={!logRunning}
                  onClick={() => {
                    StopLogcat();
                    setLogRunning(false);
                  }}
                >
                  停止
                </button>
                <button
                  className="btn"
                  onClick={() => {
                    if (logPaused) {
                      setLogLines((prev) => {
                        const next = prev.concat(logBuffer.current);
                        logBuffer.current = [];
                        return next.length > 50000 ? next.slice(-40000) : next;
                      });
                    }
                    setLogPaused((p) => !p);
                  }}
                >
                  {logPaused ? "继续" : "暂停"}
                </button>
                <button className="btn" onClick={() => setLogLines([])}>
                  清空显示
                </button>
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={autoScroll}
                    onChange={(e) => setAutoScroll(e.target.checked)}
                  />
                  滚底
                </label>
                <input
                  placeholder="关键字过滤"
                  value={logFilter}
                  onChange={(e) => setLogFilter(e.target.value)}
                  style={{ minWidth: 160 }}
                />
                {LOG_LEVELS.map((lv) => (
                  <label key={lv} className="chk log-level-chk">
                    <input
                      type="checkbox"
                      checked={!!logLevels[lv]}
                      onChange={(e) => setLogLevels((s) => ({ ...s, [lv]: e.target.checked }))}
                    />
                    <span className={`log-level-swatch lv-${lv}`} aria-hidden />
                    <span className={`log-level-label lv-${lv}`}>{lv}</span>
                  </label>
                ))}
                <span className="muted">
                  {logRunning ? "抓取中" : "已停止"} · 显示 {filteredLogs.length} / {logLines.length}
                </span>
              </div>
              <div className="log-view">
                {filteredLogs.slice(-2000).map((line, i) => {
                  const lv = parseLevel(line);
                  return (
                    <div key={i} className={`log-line lv-${lv}`}>
                      {line}
                    </div>
                  );
                })}
                <div ref={logEndRef} />
              </div>
            </section>
          )}

          {tab === "settings" && settings && (
            <section className="panel">
              <h2>设置</h2>
              <div className="form">
                <label>
                  主题
                  <select
                    value={settings.theme}
                    onChange={(e) => setSettings({ ...settings, theme: e.target.value })}
                  >
                    <option value="dark">深色</option>
                    <option value="light">浅色</option>
                  </select>
                </label>
                <label>
                  批量并发数
                  <input
                    type="number"
                    min={1}
                    max={16}
                    value={settings.maxConcurrency}
                    onChange={(e) =>
                      setSettings({ ...settings, maxConcurrency: Number(e.target.value) || 4 })
                    }
                  />
                </label>
                <h3>scrcpy 默认参数</h3>
                <label>
                  最大边长（0=不限）
                  <input
                    type="number"
                    value={settings.scrcpyMaxSize}
                    onChange={(e) =>
                      setSettings({ ...settings, scrcpyMaxSize: Number(e.target.value) || 0 })
                    }
                  />
                </label>
                <label>
                  码率
                  <input
                    value={settings.scrcpyBitRate}
                    onChange={(e) => setSettings({ ...settings, scrcpyBitRate: e.target.value })}
                  />
                </label>
                <label>
                  最大 FPS（0=不限）
                  <input
                    type="number"
                    value={settings.scrcpyMaxFps}
                    onChange={(e) =>
                      setSettings({ ...settings, scrcpyMaxFps: Number(e.target.value) || 0 })
                    }
                  />
                </label>
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={settings.scrcpyStayAwake}
                    onChange={(e) =>
                      setSettings({ ...settings, scrcpyStayAwake: e.target.checked })
                    }
                  />
                  保持常亮
                </label>
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={settings.scrcpyNoAudio}
                    onChange={(e) => setSettings({ ...settings, scrcpyNoAudio: e.target.checked })}
                  />
                  关闭音频
                </label>
                <label className="chk">
                  <input
                    type="checkbox"
                    checked={settings.killScrcpyOnExit}
                    onChange={(e) =>
                      setSettings({ ...settings, killScrcpyOnExit: e.target.checked })
                    }
                  />
                  退出时结束全部 scrcpy
                </label>
                <div className="row gap">
                  <button
                    className="btn primary"
                    onClick={() =>
                      run("保存设置", async () => {
                        // 固定使用内置 bin/，不再允许自定义路径
                        const next = { ...settings, adbPath: "", scrcpyPath: "" };
                        setSettings(next);
                        await SaveSettings(next);
                        setPaths((await GetToolPaths()) as { adb: string; scrcpy: string });
                      })
                    }
                  >
                    保存
                  </button>
                  <button
                    className="btn"
                    onClick={() =>
                      run("测试 adb", async () => {
                        const v = await TestAdb();
                        window.alert(v || "adb 正常");
                      })
                    }
                  >
                    测试 adb
                  </button>
                </div>
                {paths && (
                  <div className="paths mono">
                    <div className="muted" style={{ marginBottom: 4 }}>
                      工具路径（内置固定）
                    </div>
                    <div>adb: {paths.adb}</div>
                    <div>scrcpy: {paths.scrcpy}</div>
                  </div>
                )}
              </div>
            </section>
          )}
        </div>
      </main>
    </div>
  );
}
