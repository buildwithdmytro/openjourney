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
      tenant: "tenant-1",
      app: "app-1",
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
      endpoint: "https://events.example.test", apiKey: "key", tenant: "tenant-1", app: "app-1", storage,
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
      tenant: "tenant-1",
      app: "app-1",
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
      tenant: "tenant-1",
      app: "app-1",
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
      tenant: "tenant-1",
      app: "app-1",
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

  it("fetches anonymous inbox without token", async () => {
    let sequence = 0;
    const mockMessages = [
      { id: "msg-1", message_type: "modal", status: "delivered", rank: 1 },
      { id: "msg-2", message_type: "card", status: "delivered", rank: 0 },
    ];
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ messages: mockMessages }),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
      randomUUID: () => `anon-${++sequence}`,
    });

    const messages = await client.fetchInbox();
    expect(messages).toEqual(mockMessages);
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("/v1/messages/inbox?tenant=tenant-1&app=app-1&anonymous_id=anon-1"),
      expect.objectContaining({
        method: "GET",
        headers: expect.objectContaining({ Authorization: "Bearer public-key" }),
      }),
    );
    client.destroy();
  });

  it("fetches inbox with token for identified user", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        messages: [{ id: "msg-1", message_type: "modal", status: "delivered" }],
      }),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    client.identify("user-1");
    const messages = await client.fetchInbox("signed-token-abc");
    expect(messages).toHaveLength(1);
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("/v1/messages/inbox?tenant=tenant-1&app=app-1&token=signed-token-abc&external_id=user-1"),
      expect.any(Object),
    );
    client.destroy();
  });

  it("requires token for identified user to fetch inbox", async () => {
    const request = vi.fn();
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    client.identify("user-1");
    await expect(client.fetchInbox()).rejects.toThrow("identified user requires a token");
    expect(request).not.toHaveBeenCalled();
    client.destroy();
  });

  it("reports impression on anonymous message", async () => {
    let sequence = 0;
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
      randomUUID: () => `anon-${++sequence}`,
    });

    await client.reportImpression("msg-1");
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("/v1/messages/msg-1/impression?tenant=tenant-1&app=app-1&anonymous_id=anon-1"),
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ Authorization: "Bearer public-key" }),
      }),
    );
    client.destroy();
  });

  it("reports click with token for identified user", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    client.identify("user-1");
    await client.reportClick("msg-1", "signed-token");
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("/v1/messages/msg-1/click?tenant=tenant-1&app=app-1&token=signed-token&external_id=user-1"),
      expect.any(Object),
    );
    client.destroy();
  });

  it("reports dismiss on message", async () => {
    let sequence = 0;
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
      randomUUID: () => `anon-${++sequence}`,
    });

    await client.reportDismiss("msg-1");
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("/v1/messages/msg-1/dismiss?tenant=tenant-1&app=app-1&anonymous_id=anon-1"),
      expect.objectContaining({
        method: "POST",
        body: "{}",
      }),
    );
    client.destroy();
  });

  it("requires token for identified user to report engagement", async () => {
    const request = vi.fn();
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    client.identify("user-1");
    await expect(client.reportImpression("msg-1")).rejects.toThrow("identified user requires a token");
    await expect(client.reportClick("msg-1")).rejects.toThrow("identified user requires a token");
    await expect(client.reportDismiss("msg-1")).rejects.toThrow("identified user requires a token");
    expect(request).not.toHaveBeenCalled();
    client.destroy();
  });

  it("handles 401 unauthorized on inbox fetch", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "invalid-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    await expect(client.fetchInbox()).rejects.toThrow("Unauthorized");
    client.destroy();
  });

  it("handles 404 not found on engagement report", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-1",
      app: "app-1",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    await expect(client.reportImpression("nonexistent")).rejects.toThrow("message not found");
    client.destroy();
  });

  it("sends required tenant and app params in fetchInbox", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ messages: [] }),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-123",
      app: "app-456",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    await client.fetchInbox();
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("tenant=tenant-123"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("app=app-456"),
      expect.any(Object),
    );
    client.destroy();
  });

  it("sends external_id in fetchInbox with token for identified user", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ messages: [] }),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-123",
      app: "app-456",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    client.identify("user-789");
    await client.fetchInbox("signed-token");
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("tenant=tenant-123"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("app=app-456"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("external_id=user-789"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("token=signed-token"),
      expect.any(Object),
    );
    client.destroy();
  });

  it("sends required tenant and app params in reportEngagement", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-123",
      app: "app-456",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    await client.reportImpression("msg-1");
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("tenant=tenant-123"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("app=app-456"),
      expect.any(Object),
    );
    client.destroy();
  });

  it("sends external_id in reportEngagement with token for identified user", async () => {
    const request = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    });
    const client = new OpenJourney({
      endpoint: "https://events.example.test",
      apiKey: "public-key",
      tenant: "tenant-123",
      app: "app-456",
      storage: new MemoryStorage(),
      fetch: request,
      flushIntervalMs: 0,
    });

    client.identify("user-789");
    await client.reportClick("msg-1", "signed-token");
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("tenant=tenant-123"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("app=app-456"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("external_id=user-789"),
      expect.any(Object),
    );
    expect(request).toHaveBeenCalledWith(
      expect.stringContaining("token=signed-token"),
      expect.any(Object),
    );
    client.destroy();
  });
});
