import { describe, expect, it, vi } from "vitest";
import { OpenJourney } from "./index";

class MemoryStorage implements Storage {
  private values = new Map<string, string>();
  get length() { return this.values.size; }
  clear() { this.values.clear(); }
  getItem(key: string) { return this.values.get(key) ?? null; }
  key(index: number) { return [...this.values.keys()][index] ?? null; }
  removeItem(key: string) { this.values.delete(key); }
  setItem(key: string, value: string) { this.values.set(key, value); }
}

describe("OpenJourney", () => {
  it("batches identity, profile, and consent with stable retry data", async () => {
    const storage = new MemoryStorage();
    const request = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 503 })
      .mockResolvedValueOnce({ ok: true, status: 202 });
    let sequence = 0;
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "key",
      storage,
      fetch: request,
      flushIntervalMs: 0,
      now: () => new Date("2026-01-01T00:00:00Z"),
      randomUUID: () => `id-${++sequence}`,
    });
    client.identify("customer-1");
    client.setAttributes({ plan: "pro" });
    client.setConsent("email", "subscribed", { evidence: { form: "signup" } });
    await expect(client.flush()).rejects.toThrow("503");
    await client.flush();
    expect(request).toHaveBeenCalledTimes(2);
    const firstBody = request.mock.calls[0][1]?.body;
    const retryBody = request.mock.calls[1][1]?.body;
    expect(retryBody).toEqual(firstBody);
    const parsed = JSON.parse(String(firstBody));
    expect(parsed.events).toHaveLength(2);
    expect(parsed.events[0].external_id).toBe("customer-1");
    expect(parsed.events[0].anonymous_id).toBe("id-1");
    expect(storage.getItem("openjourney:event-queue:v1")).toBe("[]");
    client.destroy();
  });

  it("restores queued events after a new client instance", () => {
    const storage = new MemoryStorage();
    const options = {
      endpoint: "https://events.example.test", apiKey: "key", storage,
      fetch: vi.fn(), flushIntervalMs: 0, randomUUID: () => "anonymous",
    };
    const first = new OpenJourney(options);
    first.track("product.viewed", { sku: "123" });
    first.destroy();
    const second = new OpenJourney(options);
    expect(storage.getItem("openjourney:event-queue:v1")).toContain("product.viewed");
    second.destroy();
  });

  it("emits explicit alias and merge commands", async () => {
    const request = vi.fn().mockResolvedValue({ ok: true, status: 202 });
    let sequence = 0;
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "key",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
      now: () => new Date("2026-01-01T00:00:00Z"),
      randomUUID: () => `id-${++sequence}`,
    });

    client.identify("customer-target");
    client.alias("email", "ada@example.test");
    client.merge("customer-source");
    await client.flush();

    const parsed = JSON.parse(String(request.mock.calls[0][1]?.body));
    expect(parsed.events.map((event: { event_type: string }) => event.event_type)).toEqual([
      "identity.alias",
      "identity.merge",
    ]);
    expect(parsed.events[0].payload).toEqual({ namespace: "email", value: "ada@example.test" });
    expect(parsed.events[1].external_id).toBe("customer-target");
    expect(parsed.events[1].payload).toEqual({ source_external_id: "customer-source" });
    client.destroy();
  });

  it("resets anonymous identity and clears external identity", () => {
    let sequence = 0;
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "key",
      storage: new MemoryStorage(),
      fetch: vi.fn(),
      flushIntervalMs: 0,
      randomUUID: () => `id-${++sequence}`,
    });

    expect(client.getAnonymousId()).toBe("id-1");
    client.identify("customer-1");
    client.reset();
    expect(client.getAnonymousId()).toBe("id-2");
    expect(client.getExternalId()).toBeUndefined();
    client.destroy();
  });

  it("auto-flushes when capped batch size is reached", async () => {
    const request = vi.fn().mockResolvedValue({ ok: true, status: 202 });
    let sequence = 0;
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "key",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
      batchSize: 2,
      randomUUID: () => `id-${++sequence}`,
    });

    client.track("product.viewed", { sku: "1" });
    client.track("product.viewed", { sku: "2" });
    await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(1));
    const parsed = JSON.parse(String(request.mock.calls[0][1]?.body));
    expect(parsed.events).toHaveLength(2);
    client.destroy();
  });
});
