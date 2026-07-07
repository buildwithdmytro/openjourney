import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

const authMock = vi.hoisted(() => ({
  restoreOIDCSession: vi.fn<() => Promise<string | null>>(),
  signIn: vi.fn<() => Promise<void>>(),
  signOut: vi.fn<() => Promise<void>>(),
}));

vi.mock("./auth", () => ({
  oidcConfigured: true,
  restoreOIDCSession: authMock.restoreOIDCSession,
  signIn: authMock.signIn,
  signOut: authMock.signOut,
}));

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: () => Promise.resolve(body) });
}

const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input);
  if (url.endsWith("/health/ready")) return Promise.resolve({ ok: true });
  if (url.includes("/v1/auth/login")) return jsonResponse({
    access_token: "session-token",
    token_type: "Bearer",
    expires_at: "2026-07-07T13:00:00Z",
  });
  if (url.includes("/v1/auth/logout")) return Promise.resolve({ ok: true, status: 204, json: () => Promise.resolve({}) });
  if (url.includes("/v1/schemas")) return jsonResponse({ schemas: [] });
  if (url.includes("/v1/api-keys") && init?.method === "POST") {
    return jsonResponse({
      api_key: { id: "key-2", name: "Website", scopes: ["events:write"], expires_at: "2026-07-08T12:00:00Z", created_at: "2026-07-07T12:00:00Z" },
      secret: "oj_secret",
    }, 201);
  }
  if (url.includes("/v1/api-keys")) return jsonResponse({ api_keys: [] });
  if (url.includes("/v1/privacy/requests") && init?.method === "POST") {
    return jsonResponse({ id: "privacy-1", external_id: "customer-1", request_type: "export", status: "pending", created_at: "2026-01-01T00:00:00Z" }, 202);
  }
  if (url.includes("/v1/roles")) return jsonResponse({ roles: [{ id: "role-1", name: "Operator", permissions: ["profiles:read"], system: false, created_at: "2026-01-01T00:00:00Z" }] });
  if (url.includes("/v1/users")) return jsonResponse({ users: [] });
  if (url.includes("/v1/operations/queues")) return jsonResponse({ queues: [{ queue: "projection", pending: 1, processing: 0, dead: 0 }] });
  if (url.includes("/v1/operations/dlq")) return jsonResponse({ dead_letters: [] });
  if (url.includes("/v1/audit")) return jsonResponse({ audit_events: [{ id: "audit-1", actor_type: "api_key", actor_id: "key-1", action: "events.accept", resource_type: "event_batch", metadata: {}, occurred_at: "2026-01-01T00:00:00Z" }] });
  return jsonResponse({});
});

vi.stubGlobal("fetch", fetchMock);

describe("App", () => {
  beforeEach(() => {
    cleanup();
    fetchMock.mockClear();
    authMock.restoreOIDCSession.mockReset();
    authMock.restoreOIDCSession.mockResolvedValue(null);
    authMock.signIn.mockReset();
    authMock.signIn.mockResolvedValue();
    authMock.signOut.mockReset();
    authMock.signOut.mockResolvedValue();
    sessionStorage.clear();
    localStorage.clear();
    localStorage.setItem("oj_api_key", "test-key");
  });

  it("renders the profile lookup", () => {
    render(<App />);
    expect(screen.getByRole("heading", { name: "Profiles" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Find profile" })).toBeInTheDocument();
  });

  it("exposes privacy request operations", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Privacy" }));
    fireEvent.change(screen.getByLabelText("External ID"), { target: { value: "customer-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Submit privacy request" }));
    await screen.findByText("export · pending");
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/privacy/requests"), expect.objectContaining({ method: "POST" }));
  });

  it("exposes access administration", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Access" }));
    await screen.findByText("Operator");
    expect(screen.getByRole("button", { name: "Create role" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Provision user" })).toBeInTheDocument();
  });

  it("creates API keys with optional expiration", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "API keys" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Website" } });
    fireEvent.change(screen.getByLabelText("Scopes"), { target: { value: "events:write" } });
    fireEvent.change(screen.getByLabelText("Expires at"), { target: { value: "2026-07-08T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Create scoped key" }));
    await screen.findByText("Copy this secret now.");
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/api-keys"), expect.objectContaining({
      method: "POST",
      body: expect.stringContaining("\"expires_at\""),
    }));
  });

  it("exposes DLQ and audit operations", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Operations" }));
    await screen.findByText("DLQ actions");
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/operations/dlq"), expect.any(Object));

    fireEvent.click(screen.getByRole("button", { name: "Audit" }));
    await screen.findByText("events.accept");
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/audit?limit=100"), expect.any(Object)));
  });

  it("persists manually entered API keys", async () => {
    render(<App />);
    const credential = screen.getByLabelText("API key");
    fireEvent.change(credential, { target: { value: "manual-key" } });
    await waitFor(() => expect(localStorage.getItem("oj_api_key")).toBe("manual-key"));
  });

  it("does not persist restored OIDC tokens to localStorage", async () => {
    authMock.restoreOIDCSession.mockResolvedValue("oidc-id-token");
    render(<App />);
    await waitFor(() => expect(screen.getByLabelText("API key")).toHaveValue("oidc-id-token"));
    await waitFor(() => expect(localStorage.getItem("oj_api_key")).toBeNull());
  });

  it("stores local login sessions in sessionStorage and revokes them on sign out", async () => {
    render(<App />);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "admin@example.test" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: "Log in" }));
    await waitFor(() => expect(sessionStorage.getItem("oj_session_token")).toBe("session-token"));
    expect(localStorage.getItem("oj_api_key")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(sessionStorage.getItem("oj_session_token")).toBeNull());
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/auth/logout"), expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({ Authorization: "Bearer session-token" }),
    }));
  });
});
