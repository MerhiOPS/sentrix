import { describe, expect, it } from "vitest";
import { TimeBuffer, type Point } from "./ringbuffer";

const p = (s: number, cpu: number): Point => ({ tsMs: s * 1000, values: { cpu } });

describe("TimeBuffer", () => {
  it("dedupes reconnect backfill by timestamp (OLD B7)", () => {
    const b = new TimeBuffer(100);
    b.upsert([p(1, 10), p(2, 20), p(3, 30)]);
    b.upsert([p(2, 21), p(3, 31), p(4, 40)]); // backfill overlaps
    expect(b.size()).toBe(4);
    expect(b.columns(["cpu"])).toEqual([[1, 2, 3, 4], [10, 21, 31, 40]]);
  });

  it("keeps order when samples arrive out of order", () => {
    const b = new TimeBuffer(10);
    b.upsert([p(5, 5), p(1, 1), p(3, 3)]);
    expect(b.columns(["cpu"])[0]).toEqual([1, 3, 5]);
    expect(b.latestMs()).toBe(5000);
  });

  it("exposes the newest point", () => {
    const b = new TimeBuffer(5);
    expect(b.latest()).toBeUndefined();
    b.upsert([p(2, 20), p(1, 10)]);
    expect(b.latest()?.values.cpu).toBe(20);
  });

  it("is bounded, dropping the oldest", () => {
    const b = new TimeBuffer(3);
    b.upsert([p(1, 1), p(2, 2), p(3, 3), p(4, 4)]);
    expect(b.columns(["cpu"])[0]).toEqual([2, 3, 4]);
  });

  it("reports missing values as null, not zero", () => {
    const b = new TimeBuffer(3);
    b.upsert([{ tsMs: 1000, values: {} }]);
    expect(b.columns(["cpu"])[1]).toEqual([null]);
    expect(new TimeBuffer(3).latestMs()).toBe(0);
  });
});
