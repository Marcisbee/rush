export type SnapshotUpdateMode = "none" | "new" | "all";

export interface SnapshotOptions {
  values?: Readonly<Record<string, string>>;
  update?: SnapshotUpdateMode;
  onUpdate?: (key: string, value: string) => void;
}

let values = new Map<string, string>();
let updateMode: SnapshotUpdateMode = "new";
let onUpdate: SnapshotOptions["onUpdate"];
let currentTest = "unknown test";
let counter = 0;

export function configureSnapshots(options: SnapshotOptions = {}): void {
  values = new Map(Object.entries(options.values ?? {}));
  updateMode = options.update ?? "new";
  onUpdate = options.onUpdate;
}

export function enterSnapshotTest(fullName: string): void {
  currentTest = fullName;
  counter = 0;
}

export function leaveSnapshotTest(): void {
  currentTest = "unknown test";
  counter = 0;
}

export function snapshotKey(name?: string): string {
  counter += 1;
  return `${currentTest} > ${name ?? counter}`;
}

export function matchSnapshot(received: unknown, name?: string): void {
  const key = snapshotKey(name);
  const serialized = serialize(received);
  const expected = values.get(key);

  if (expected === serialized) return;
  if (updateMode === "all" || (expected === undefined && updateMode === "new")) {
    values.set(key, serialized);
    onUpdate?.(key, serialized);
    return;
  }

  if (expected === undefined) {
    throw new Error(`Snapshot \"${key}\" does not exist`);
  }
  throw new Error(`Snapshot \"${key}\" did not match\nExpected: ${expected}\nReceived: ${serialized}`);
}

export function getSnapshotValues(): Readonly<Record<string, string>> {
  return Object.fromEntries(values);
}

export function serialize(value: unknown, seen = new WeakSet<object>()): string {
  if (value === null) return "null";
  if (value === undefined) return "undefined";
  if (typeof value === "string") return JSON.stringify(value);
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return String(value);
  }
  if (typeof value === "symbol") return String(value);
  if (typeof value === "function") return `[Function ${value.name || "anonymous"}]`;

  if (seen.has(value)) return "[Circular]";
  seen.add(value);

  if (value instanceof Date) return `Date(${value.toISOString()})`;
  if (value instanceof RegExp) return String(value);
  if (typeof Element !== "undefined" && value instanceof Element) {
    return value.outerHTML;
  }
  if (Array.isArray(value)) {
    return `[${value.map((item) => serialize(item, seen)).join(", ")}]`;
  }
  if (value instanceof Map) {
    return `Map {${[...value.entries()].map(([key, item]) => `${serialize(key, seen)} => ${serialize(item, seen)}`).join(", ")}}`;
  }
  if (value instanceof Set) {
    return `Set {${[...value].map((item) => serialize(item, seen)).join(", ")}}`;
  }

  const object = value as Record<string, unknown>;
  const entries = Object.keys(object)
    .sort()
    .map((key) => `${JSON.stringify(key)}: ${serialize(object[key], seen)}`);
  return `{${entries.join(", ")}}`;
}
