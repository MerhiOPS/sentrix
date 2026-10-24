import { describe, expect, it } from "vitest";
import { naturalCompare } from "./naturalSort";

describe("naturalCompare", () => {
  it("orders numbers by value, not text", () => {
    expect(["10", "2", "1", "0", "11"].sort(naturalCompare)).toEqual(["0", "1", "2", "10", "11"]);
    expect(["eth10", "eth2", "eth1"].sort(naturalCompare)).toEqual(["eth1", "eth2", "eth10"]);
  });
});
