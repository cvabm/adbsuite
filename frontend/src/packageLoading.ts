export type PackageMetadata = {
  name: string;
  path?: string;
  label?: string;
  labelPending?: boolean;
  versionName?: string;
  versionCode?: number;
};

// Never replace state flags or resurrect an app removed while metadata was loading.
// Separate fields also prevent a late label response from clearing fresh versions.
export function mergePackageMetadata<T extends PackageMetadata>(
  current: T[], updates: PackageMetadata[], kind: "labels" | "versions",
): T[] {
  const byName = new Map(updates.map((p) => [p.name, p]));
  return current.map((p) => {
    const update = byName.get(p.name);
    if (!update || update.path !== p.path) return p;
    return kind === "labels"
      ? { ...p, label: update.label || p.label, labelPending: false }
      : { ...p, versionName: update.versionName, versionCode: update.versionCode };
  });
}

// Names arrive in batches; versions do not wait behind uncached APK resources.
// Invalidated refreshes neither publish nor schedule further label batches.
export async function loadPackageDetails<T extends PackageMetadata>(
  list: T[],
  current: () => boolean,
  labels: (batch: T[]) => Promise<PackageMetadata[]>,
  versions: (list: T[]) => Promise<PackageMetadata[]>,
  publish: (kind: "labels" | "versions", updates: PackageMetadata[]) => void,
): Promise<string[]> {
  if (!current()) return [];
  const pending = list.filter((p) => p.labelPending);
  const results = await Promise.allSettled([
    (async () => {
      for (let i = 0; i < pending.length && current(); i += 24) {
        const updates = await labels(pending.slice(i, i + 24));
        if (current()) publish("labels", updates || []);
      }
    })(),
    (async () => {
      if (!list.length) return;
      const updates = await versions(list);
      if (current()) publish("versions", updates || []);
    })(),
  ]);
  if (!current()) return [];
  return results.flatMap((r) => r.status === "rejected" ? [String(r.reason)] : []);
}
