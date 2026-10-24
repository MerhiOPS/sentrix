const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

/** Compares "core 2" before "core 10", "eth0" before "eth1". */
export function naturalCompare(a: string, b: string): number {
  return collator.compare(a, b);
}
