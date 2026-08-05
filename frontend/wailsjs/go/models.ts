export namespace adb {
	
	export class Device {
	    serial: string;
	    state: string;
	    model: string;
	    product: string;
	    transportId: string;
	    isWireless: boolean;
	    usb: string;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.state = source["state"];
	        this.model = source["model"];
	        this.product = source["product"];
	        this.transportId = source["transportId"];
	        this.isWireless = source["isWireless"];
	        this.usb = source["usb"];
	    }
	}
	export class PackageInfo {
	    name: string;
	    label: string;
	    path?: string;
	    system: boolean;
	    versionName?: string;
	    versionCode?: number;
	    disabled?: boolean;
	    uninstalled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PackageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.path = source["path"];
	        this.system = source["system"];
	        this.versionName = source["versionName"];
	        this.versionCode = source["versionCode"];
	        this.disabled = source["disabled"];
	        this.uninstalled = source["uninstalled"];
	    }
	}
	export class RecordSession {
	    serial: string;
	    localPath: string;
	    remote: string;
	    startedAt: number;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RecordSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.localPath = source["localPath"];
	        this.remote = source["remote"];
	        this.startedAt = source["startedAt"];
	        this.running = source["running"];
	    }
	}
	export class RemoteEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    isLink: boolean;
	    size: number;
	    mode: string;
	    mtime: string;
	    link?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.isLink = source["isLink"];
	        this.size = source["size"];
	        this.mode = source["mode"];
	        this.mtime = source["mtime"];
	        this.link = source["link"];
	    }
	}
	export class RunningService {
	    package: string;
	    label?: string;
	    service: string;
	    component: string;
	    process?: string;
	    pid?: number;
	    userId: number;
	    client?: string;
	    foreground: boolean;
	    startRequested: boolean;
	    system: boolean;
	    createTime?: string;
	    lastActivity?: string;
	    baseDir?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunningService(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.package = source["package"];
	        this.label = source["label"];
	        this.service = source["service"];
	        this.component = source["component"];
	        this.process = source["process"];
	        this.pid = source["pid"];
	        this.userId = source["userId"];
	        this.client = source["client"];
	        this.foreground = source["foreground"];
	        this.startRequested = source["startRequested"];
	        this.system = source["system"];
	        this.createTime = source["createTime"];
	        this.lastActivity = source["lastActivity"];
	        this.baseDir = source["baseDir"];
	    }
	}

}

export namespace main {
	
	export class BootstrapData {
	    settings: settings.Settings;
	    paths: Record<string, string>;
	    devices: adb.Device[];
	    scrcpy: scrcpy.Session[];
	    tasks: tasks.Item[];
	    adbOk: boolean;
	    adbMsg: string;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], settings.Settings);
	        this.paths = source["paths"];
	        this.devices = this.convertValues(source["devices"], adb.Device);
	        this.scrcpy = this.convertValues(source["scrcpy"], scrcpy.Session);
	        this.tasks = this.convertValues(source["tasks"], tasks.Item);
	        this.adbOk = source["adbOk"];
	        this.adbMsg = source["adbMsg"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace scrcpy {
	
	export class Session {
	    serial: string;
	    pid: number;
	    args: string;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.pid = source["pid"];
	        this.args = source["args"];
	    }
	}

}

export namespace settings {
	
	export class Settings {
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
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.adbPath = source["adbPath"];
	        this.scrcpyPath = source["scrcpyPath"];
	        this.theme = source["theme"];
	        this.maxConcurrency = source["maxConcurrency"];
	        this.scrcpyMaxSize = source["scrcpyMaxSize"];
	        this.scrcpyBitRate = source["scrcpyBitRate"];
	        this.scrcpyMaxFps = source["scrcpyMaxFps"];
	        this.scrcpyStayAwake = source["scrcpyStayAwake"];
	        this.scrcpyNoAudio = source["scrcpyNoAudio"];
	        this.killScrcpyOnExit = source["killScrcpyOnExit"];
	        this.recentHosts = source["recentHosts"];
	        this.lastDevice = source["lastDevice"];
	        this.tcpipPort = source["tcpipPort"];
	    }
	}

}

export namespace tasks {
	
	export class Item {
	    id: string;
	    kind: string;
	    serial: string;
	    label: string;
	    status: string;
	    message: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.serial = source["serial"];
	        this.label = source["label"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

