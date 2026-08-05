import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import {
  DeleteRemote,
  ListRemoteEntries,
  MkdirRemote,
  PullFile,
  PushFile,
  RenameRemote,
  SelectDirectory,
  SelectFile,
  SelectSaveFile,
} from "../../wailsjs/go/main/App";

export type RemoteEntry = {
  name: string;
  path: string;
  isDir: boolean;
  isLink: boolean;
  size: number;
  mode: string;
  mtime: string;
  link?: string;
};

type Props = {
  serial: string;
  busy: boolean;
  setBusy: (v: boolean) => void;
  log: (msg: string) => void;
};

const QUICK_PATHS: { label: string; path: string }[] = [
  { label: "内部存储", path: "/sdcard" },
  { label: "Download", path: "/sdcard/Download" },
  { label: "DCIM", path: "/sdcard/DCIM" },
  { label: "Pictures", path: "/sdcard/Pictures" },
  { label: "Movies", path: "/sdcard/Movies" },
  { label: "tmp", path: "/data/local/tmp" },
  { label: "根目录 /", path: "/" },
  { label: "storage", path: "/storage" },
];

function normalizePath(p: string): string {
  let s = (p || "").trim().replace(/\\/g, "/");
  if (!s) return "/sdcard";
  while (s.includes("//")) s = s.replace(/\/\//g, "/");
  if (!s.startsWith("/")) s = "/" + s;
  if (s.length > 1 && s.endsWith("/")) s = s.replace(/\/+$/, "");
  return s || "/";
}

function parentPath(p: string): string {
  const n = normalizePath(p);
  if (n === "/") return "/";
  const i = n.lastIndexOf("/");
  return i <= 0 ? "/" : n.slice(0, i);
}

function joinPath(dir: string, name: string): string {
  const d = normalizePath(dir);
  const n = name.replace(/^\/+|\/+$/g, "");
  if (!n || n === ".") return d;
  if (n === "..") return parentPath(d);
  return d === "/" ? "/" + n : d + "/" + n;
}

function formatSize(n: number, isDir: boolean): string {
  if (isDir) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function entryIcon(e: RemoteEntry): string {
  if (e.isLink) return "🔗";
  if (e.isDir) return "📁";
  const lower = e.name.toLowerCase();
  if (/\.(png|jpe?g|gif|webp|bmp|heic)$/.test(lower)) return "🖼";
  if (/\.(mp4|mkv|avi|mov|webm)$/.test(lower)) return "🎬";
  if (/\.(mp3|m4a|aac|wav|flac|ogg)$/.test(lower)) return "🎵";
  if (/\.(apk)$/.test(lower)) return "📦";
  if (/\.(zip|rar|7z|tar|gz)$/.test(lower)) return "🗜";
  if (/\.(txt|log|md|json|xml|csv)$/.test(lower)) return "📄";
  return "📃";
}

type CtxMenu = {
  x: number;
  y: number;
  entry: RemoteEntry | null; // null = blank area
};

export default function DeviceExplorer({ serial, busy, setBusy, log }: Props) {
  const [cwd, setCwd] = useState("/sdcard");
  const [pathInput, setPathInput] = useState("/sdcard");
  const [entries, setEntries] = useState<RemoteEntry[]>([]);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [ctx, setCtx] = useState<CtxMenu | null>(null);
  const lastSerial = useRef("");
  const listRef = useRef<HTMLDivElement>(null);

  const run = useCallback(
    async <T,>(label: string, fn: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true);
      try {
        const r = await fn();
        log(`${label}: 完成`);
        return r;
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        log(`${label}: 失败 — ${msg}`);
        setError(msg);
        return undefined;
      } finally {
        setBusy(false);
      }
    },
    [log, setBusy]
  );

  const refresh = useCallback(
    async (path?: string, silent = false) => {
      if (!serial) {
        setEntries([]);
        setError("请先选择设备");
        return;
      }
      const target = normalizePath(path ?? cwd);
      setLoading(true);
      setError("");
      try {
        const list = ((await ListRemoteEntries(serial, target)) as RemoteEntry[]) || [];
        setEntries(list);
        setCwd(target);
        setPathInput(target);
        setSelected({});
        if (!silent) {
          log(`列出 ${target} · ${list.length} 项`);
        }
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        setError(msg);
        setEntries([]);
        if (!silent) log(`列出失败: ${msg}`);
      } finally {
        setLoading(false);
      }
    },
    [serial, cwd, log]
  );

  // Load when device changes or first mount with device
  useEffect(() => {
    if (!serial) {
      setEntries([]);
      setError("请先选择设备");
      return;
    }
    if (lastSerial.current !== serial) {
      lastSerial.current = serial;
      // reset to common start path
      void refresh("/sdcard", true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serial]);

  // Close context menu on outside click
  useEffect(() => {
    if (!ctx) return;
    const close = () => setCtx(null);
    window.addEventListener("click", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [ctx]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter((e) => e.name.toLowerCase().includes(q));
  }, [entries, filter]);

  const selectedEntries = useMemo(
    () => entries.filter((e) => selected[e.path]),
    [entries, selected]
  );

  const crumbs = useMemo(() => {
    const p = normalizePath(cwd);
    if (p === "/") return [{ label: "/", path: "/" }];
    const parts = p.split("/").filter(Boolean);
    const out: { label: string; path: string }[] = [{ label: "/", path: "/" }];
    let acc = "";
    for (const part of parts) {
      acc += "/" + part;
      out.push({ label: part, path: acc });
    }
    return out;
  }, [cwd]);

  const goTo = (path: string) => {
    void refresh(path);
  };

  const goUp = () => {
    void refresh(parentPath(cwd));
  };

  const openEntry = (e: RemoteEntry) => {
    if (e.isDir) {
      goTo(e.path);
      return;
    }
    // file: select only
    setSelected({ [e.path]: true });
  };

  const toggleSelect = (path: string, multi: boolean) => {
    setSelected((prev) => {
      if (multi) {
        return { ...prev, [path]: !prev[path] };
      }
      return { [path]: true };
    });
  };

  const selectAll = () => {
    const next: Record<string, boolean> = {};
    for (const e of filtered) next[e.path] = true;
    setSelected(next);
  };

  const clearSelect = () => setSelected({});

  const doUpload = async () => {
    if (!serial) return;
    const f = await SelectFile("选择要推送到设备的文件", []);
    if (!f) return;
    const base = f.replace(/\\/g, "/").split("/").pop() || "file";
    const remote = joinPath(cwd, base);
    await run("推送 " + base, async () => {
      const o = await PushFile(serial, f, remote);
      log(o);
      await refresh(cwd, true);
      return o;
    });
  };

  const doUploadTo = async (dir: string) => {
    if (!serial) return;
    const f = await SelectFile("选择要推送的文件", []);
    if (!f) return;
    const base = f.replace(/\\/g, "/").split("/").pop() || "file";
    const remote = joinPath(dir, base);
    await run("推送 " + base, async () => {
      const o = await PushFile(serial, f, remote);
      log(o);
      await refresh(cwd, true);
      return o;
    });
  };

  const doDownloadOne = async (entry: RemoteEntry) => {
    if (!serial) return;
    if (entry.isDir) {
      const dir = await SelectDirectory("选择本地保存目录（将拉取整个文件夹）");
      if (!dir) return;
      const local = dir.replace(/\\/g, "/") + "/" + entry.name;
      await run("拉取目录 " + entry.name, async () => {
        const o = await PullFile(serial, entry.path, local);
        log(o);
        return o;
      });
      return;
    }
    const f = await SelectSaveFile("保存到本地", entry.name);
    if (!f) return;
    await run("拉取 " + entry.name, async () => {
      const o = await PullFile(serial, entry.path, f);
      log(o);
      return o;
    });
  };

  const doDownloadSelected = async () => {
    if (selectedEntries.length === 0) {
      log("请先选择要下载的文件/目录");
      return;
    }
    if (selectedEntries.length === 1) {
      await doDownloadOne(selectedEntries[0]);
      return;
    }
    const dir = await SelectDirectory("选择本地保存目录");
    if (!dir) return;
    for (const e of selectedEntries) {
      const local = dir.replace(/\\/g, "/") + "/" + e.name;
      await run("拉取 " + e.name, async () => {
        const o = await PullFile(serial, e.path, local);
        log(o);
        return o;
      });
    }
  };

  const doMkdir = async () => {
    if (!serial) return;
    const name = window.prompt("新建文件夹名称", "NewFolder");
    if (!name || !name.trim()) return;
    const remote = joinPath(cwd, name.trim());
    await run("新建文件夹", async () => {
      await MkdirRemote(serial, remote);
      await refresh(cwd, true);
    });
  };

  const doRename = async (entry: RemoteEntry) => {
    if (!serial) return;
    const name = window.prompt("重命名为", entry.name);
    if (!name || !name.trim() || name.trim() === entry.name) return;
    const dest = joinPath(cwd, name.trim());
    await run("重命名", async () => {
      await RenameRemote(serial, entry.path, dest);
      await refresh(cwd, true);
    });
  };

  const doDelete = async (list: RemoteEntry[]) => {
    if (!serial || list.length === 0) return;
    const names = list.map((e) => e.name).join(", ");
    const tip =
      list.length === 1
        ? `确定删除「${list[0].name}」${list[0].isDir ? "（文件夹）" : ""}？\n${list[0].path}`
        : `确定删除选中的 ${list.length} 项？\n${names}`;
    if (!window.confirm(tip)) return;
    await run("删除", async () => {
      for (const e of list) {
        await DeleteRemote(serial, e.path, true);
        log("已删除: " + e.path);
      }
      await refresh(cwd, true);
    });
  };

  const onRowContext = (e: ReactMouseEvent, entry: RemoteEntry | null) => {
    e.preventDefault();
    e.stopPropagation();
    if (entry && !selected[entry.path]) {
      setSelected({ [entry.path]: true });
    }
    setCtx({ x: e.clientX, y: e.clientY, entry });
  };

  const disabled = busy || loading || !serial;

  return (
    <section className="panel explorer-panel">
      <div className="explorer-header">
        <h2>Device Explorer</h2>
        <span className="muted explorer-sub">设备文件浏览器 · 类似 Android Studio</span>
      </div>

      <div className="explorer-layout">
        <aside className="explorer-sidebar">
          <div className="explorer-side-title">快捷路径</div>
          {QUICK_PATHS.map((q) => (
            <button
              key={q.path}
              type="button"
              className={"explorer-quick" + (normalizePath(cwd) === normalizePath(q.path) ? " active" : "")}
              disabled={disabled}
              onClick={() => goTo(q.path)}
              title={q.path}
            >
              {q.label}
            </button>
          ))}
        </aside>

        <div className="explorer-main">
          <div className="explorer-toolbar">
            <button className="btn sm" disabled={disabled || cwd === "/"} onClick={goUp} title="上级目录">
              ↑ 上级
            </button>
            <button className="btn sm" disabled={disabled} onClick={() => refresh(cwd)} title="刷新">
              刷新
            </button>
            <button className="btn sm primary" disabled={disabled} onClick={doUpload} title="推送文件到当前目录">
              上传
            </button>
            <button
              className="btn sm"
              disabled={disabled || selectedEntries.length === 0}
              onClick={doDownloadSelected}
              title="下载选中项到本地"
            >
              下载
            </button>
            <button className="btn sm" disabled={disabled} onClick={doMkdir} title="在当前目录新建文件夹">
              新建文件夹
            </button>
            <button
              className="btn sm"
              disabled={disabled || selectedEntries.length !== 1}
              onClick={() => selectedEntries[0] && doRename(selectedEntries[0])}
            >
              重命名
            </button>
            <button
              className="btn sm danger"
              disabled={disabled || selectedEntries.length === 0}
              onClick={() => doDelete(selectedEntries)}
            >
              删除
            </button>
            <input
              className="explorer-filter"
              placeholder="过滤…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </div>

          <div className="explorer-pathbar">
            <div className="explorer-crumbs">
              {crumbs.map((c, i) => (
                <span key={c.path} className="explorer-crumb-wrap">
                  {i > 0 && <span className="explorer-crumb-sep">/</span>}
                  <button
                    type="button"
                    className="explorer-crumb"
                    disabled={disabled}
                    onClick={() => goTo(c.path)}
                  >
                    {c.label}
                  </button>
                </span>
              ))}
            </div>
            <form
              className="explorer-path-form"
              onSubmit={(e) => {
                e.preventDefault();
                goTo(pathInput);
              }}
            >
              <input
                className="mono explorer-path-input"
                value={pathInput}
                onChange={(e) => setPathInput(e.target.value)}
                spellCheck={false}
              />
              <button className="btn sm" type="submit" disabled={disabled}>
                转到
              </button>
            </form>
          </div>

          <div className="explorer-status row gap wrap">
            <span className="muted">
              {loading ? "加载中…" : `${filtered.length} 项`}
              {selectedEntries.length > 0 ? ` · 已选 ${selectedEntries.length}` : ""}
            </span>
            <button type="button" className="linkish" disabled={filtered.length === 0} onClick={selectAll}>
              全选
            </button>
            <button type="button" className="linkish" disabled={selectedEntries.length === 0} onClick={clearSelect}>
              清除选择
            </button>
            {error && <span className="explorer-error">{error}</span>}
          </div>

          <div
            className="table-wrap explorer-table-wrap"
            ref={listRef}
            onContextMenu={(e) => onRowContext(e, null)}
          >
            <table className="explorer-table">
              <thead>
                <tr>
                  <th style={{ width: 36 }}></th>
                  <th>名称</th>
                  <th style={{ width: 100 }}>大小</th>
                  <th style={{ width: 110 }}>权限</th>
                  <th style={{ width: 140 }}>修改时间</th>
                </tr>
              </thead>
              <tbody>
                {!serial && (
                  <tr>
                    <td colSpan={5} className="muted">
                      请先在顶部选择设备
                    </td>
                  </tr>
                )}
                {serial && !loading && filtered.length === 0 && (
                  <tr>
                    <td colSpan={5} className="muted">
                      {error ? "无法列出此目录" : filter ? "无匹配项" : "（空目录）"}
                    </td>
                  </tr>
                )}
                {filtered.map((e) => {
                  const isSel = !!selected[e.path];
                  return (
                    <tr
                      key={e.path}
                      className={
                        (isSel ? "row-active " : "") +
                        (e.isDir ? "explorer-dir " : "explorer-file ")
                      }
                      onClick={(ev) => toggleSelect(e.path, ev.ctrlKey || ev.metaKey || ev.shiftKey)}
                      onDoubleClick={() => openEntry(e)}
                      onContextMenu={(ev) => onRowContext(ev, e)}
                    >
                      <td className="explorer-icon">{entryIcon(e)}</td>
                      <td>
                        <span className={e.isDir ? "explorer-name dir" : "explorer-name"}>
                          {e.name}
                        </span>
                        {e.isLink && e.link ? (
                          <span className="muted explorer-link"> → {e.link}</span>
                        ) : null}
                      </td>
                      <td className="mono sm-text">{formatSize(e.size, e.isDir)}</td>
                      <td className="mono sm-text">{e.mode}</td>
                      <td className="sm-text">{e.mtime || "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          <p className="hint explorer-hint">
            双击文件夹进入 · Ctrl/Shift+单击多选 · 右键菜单 · 上传推送到当前目录 · 下载支持文件与文件夹
          </p>
        </div>
      </div>

      {ctx && (
        <div
          className="explorer-ctx"
          style={{ left: ctx.x, top: ctx.y }}
          onClick={(e) => e.stopPropagation()}
        >
          {ctx.entry?.isDir && (
            <button
              type="button"
              onClick={() => {
                setCtx(null);
                goTo(ctx.entry!.path);
              }}
            >
              打开
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              setCtx(null);
              void doUploadTo(ctx.entry?.isDir ? ctx.entry.path : cwd);
            }}
          >
            上传到{ctx.entry?.isDir ? "此文件夹" : "当前目录"}…
          </button>
          {(ctx.entry || selectedEntries.length > 0) && (
            <button
              type="button"
              onClick={() => {
                setCtx(null);
                if (ctx.entry) void doDownloadOne(ctx.entry);
                else void doDownloadSelected();
              }}
            >
              下载…
            </button>
          )}
          {ctx.entry && (
            <button
              type="button"
              onClick={() => {
                setCtx(null);
                void doRename(ctx.entry!);
              }}
            >
              重命名…
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              setCtx(null);
              void doMkdir();
            }}
          >
            新建文件夹…
          </button>
          {(ctx.entry || selectedEntries.length > 0) && (
            <button
              type="button"
              className="danger"
              onClick={() => {
                setCtx(null);
                void doDelete(ctx.entry ? [ctx.entry] : selectedEntries);
              }}
            >
              删除
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              setCtx(null);
              void refresh(cwd);
            }}
          >
            刷新
          </button>
        </div>
      )}
    </section>
  );
}
