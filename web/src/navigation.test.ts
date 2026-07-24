import { describe, expect, it } from "vitest";
import { navGroups, viewTitles } from "./navigation";

describe("navigation config", () => {
  it("defines every view exactly once and keeps infrastructure/data out of Messaging", () => {
    const items = navGroups.flatMap((group) => group.items);

    expect(items).toHaveLength(29);
    expect(new Set(items).size).toBe(items.length);
    expect(items).toHaveLength(Object.keys(viewTitles).length);
    expect(navGroups.find((group) => group.label === "Messaging")?.items).toEqual([
      "templates", "campaigns", "journeys", "experiments", "flags", "messaging",
    ]);
    expect(navGroups.find((group) => group.label === "Infrastructure")?.items).toEqual([
      "connectors", "extensions",
    ]);
    expect(navGroups.find((group) => group.label === "Data")?.items).toEqual(["schemas", "catalogs"]);
  });
});
