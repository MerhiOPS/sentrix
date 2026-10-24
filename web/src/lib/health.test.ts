import { describe, expect, it } from "vitest";
import { healthNotes } from "./health";

const ok = { buffer_depth: 0, dropped_total: 0, clock_skew_ms: 200, rejected_total: 0, conflict_total: 3 };

describe("healthNotes", () => {
  it("is quiet for a healthy agent (one batch in flight, small skew, replay conflicts)", () => {
    expect(healthNotes({ ...ok, buffer_depth: 1 })).toEqual([]);
  });
  it("reports drops, refusals, backlog and skew", () => {
    const notes = healthNotes({ ...ok, dropped_total: 1, rejected_total: 4, buffer_depth: 40, clock_skew_ms: -45_000 });
    expect(notes.map((n) => n.text)).toEqual([
      "1 batch dropped by the agent",
      "4 samples refused by the server (clock or validity)",
      "40 batches waiting on the agent (replaying)",
      "agent clock off by -45 s",
    ]);
    expect(notes[2]!.bad).toBe(false);
  });
});
