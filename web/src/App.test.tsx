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
  if (url.includes("/v1/schemas")) return jsonResponse({ schemas: [{
    id: "schema-1", event_type: "checkout.completed", version: 1, status: "active", compatibility: "backward", created_at: "2026-01-01T00:00:00Z",
    schema: { type: "object", properties: { payload: { type: "object", properties: { plan_name: { type: "string" } } } } },
  }] });
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
  if (url.includes("/v1/suppressions")) return jsonResponse(null);
  if (url.includes("/v1/journeys") && init?.method === "POST") {
    return jsonResponse({
      id: "journey-2",
      tenant_id: "tenant",
      workspace_id: "workspace",
      name: "Welcome Series",
      status: "draft",
      graph: { entry_node_id: "n1" },
      latest_version: 0,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    }, 201);
  }
  if (url.includes("/v1/journeys")) return jsonResponse({ journeys: [{
    id: "journey-1",
    tenant_id: "tenant",
    workspace_id: "workspace",
    name: "Onboarding",
    description: "Activation flow",
    status: "draft",
    graph: {
      entry_node_id: "n1",
      nodes: [
        { id: "n1", type: "entry", config: { trigger: "event", event_type: "signup" }, position: { x: 100, y: 100 } },
        { id: "n2", type: "exit", config: { reason: "completed" }, position: { x: 100, y: 300 } },
      ],
      edges: [{ from: "n1", to: "n2", branch: "" }],
    },
    latest_version: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  }] });
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
    fireEvent.click(screen.getByRole("button", { name: "Scopes" }));
    fireEvent.click(screen.getByLabelText("profiles:read"));
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

  it("lists and creates journey drafts", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Journeys" }));
    await screen.findByText("Onboarding");
    fireEvent.click(screen.getByText("+ Create journey"));
    fireEvent.change(screen.getByLabelText("Journey name"), { target: { value: "Welcome Series" } });
    fireEvent.click(screen.getByRole("button", { name: "Create and design" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/journeys"), expect.objectContaining({
      method: "POST",
      body: expect.stringContaining("\"type\":\"entry\""),
    })));
  });

  it("offers plain-language journey design steps", async () => {
    window.location.hash = "journeys";
    render(<App />);
    await screen.findByText("Onboarding");
    fireEvent.click(screen.getByRole("button", { name: /Onboarding/ }));
    expect(await screen.findByText("What happens next?")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Send a message/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Wait for an event/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Decision/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Publish journey" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /plan_name/ })).toBeInTheDocument();
  });

  it("deletes and restores journey steps with keyboard shortcuts", async () => {
    window.location.hash = "journeys";
    render(<App />);
    await screen.findByText("Onboarding");
    fireEvent.click(screen.getByRole("button", { name: /Onboarding/ }));
    const entry = await screen.findByText("Journey entry");
    fireEvent.click(entry);
    fireEvent.click(screen.getByRole("button", { name: /Send a message/ }));
    expect(screen.getByText("Choose a message template")).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "Delete" });
    expect(screen.queryByText("Choose a message template")).not.toBeInTheDocument();
    fireEvent.keyDown(window, { key: "z", ctrlKey: true });
    expect(screen.getByText("Choose a message template")).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "z", ctrlKey: true, shiftKey: true });
    expect(screen.queryByText("Choose a message template")).not.toBeInTheDocument();
  });

  it("reconnects a simple path when its middle step is deleted", async () => {
    window.location.hash = "journeys";
    render(<App />);
    await screen.findByText("Onboarding");
    fireEvent.click(screen.getByRole("button", { name: /Onboarding/ }));
    fireEvent.click(await screen.findByText("Journey entry"));
    fireEvent.click(screen.getByRole("button", { name: /Send a message/ }));
    fireEvent.keyDown(window, { key: "Delete" });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => {
      const update = fetchMock.mock.calls.find(([url, init]) => String(url).includes("/v1/journeys/journey-1") && init?.method === "PUT");
      expect(update).toBeTruthy();
      const payload = JSON.parse(String(update![1]?.body));
      expect(payload.graph.edges).toEqual([{ from: "n1", to: "n2", branch: "" }]);
    });
  });

  it("creates valid labeled paths when a decision is inserted", async () => {
    window.location.hash = "journeys";
    render(<App />);
    await screen.findByText("Onboarding");
    fireEvent.click(screen.getByRole("button", { name: /Onboarding/ }));
    fireEvent.click(await screen.findByText("Journey entry"));
    fireEvent.click(screen.getByRole("button", { name: /Decision/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => {
      const update = fetchMock.mock.calls.find(([url, init]) => String(url).includes("/v1/journeys/journey-1") && init?.method === "PUT");
      const payload = JSON.parse(String(update![1]?.body));
      const decision = payload.graph.nodes.find((node: { type: string }) => node.type === "condition");
      const decisionEdges = payload.graph.edges.filter((edge: { from: string }) => edge.from === decision.id);
      expect(decisionEdges.map((edge: { branch: string }) => edge.branch).sort()).toEqual(["false", "true"]);
      expect(payload.graph.nodes.filter((node: { type: string }) => node.type === "exit")).toHaveLength(2);
    });
  });

  it("switches journey entry to scheduled when segment lists are empty", async () => {
    window.location.hash = "journeys";
    render(<App />);
    await screen.findByText("Onboarding");
    fireEvent.click(screen.getByRole("button", { name: /Onboarding/ }));
    fireEvent.click(await screen.findByText("Journey entry"));
    fireEvent.change(screen.getByLabelText("How should customers enter?"), { target: { value: "scheduled" } });
    expect(screen.getByLabelText("Segment")).toBeInTheDocument();
    expect(screen.getByLabelText(/^Schedule/)).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "-- Select Segment --" })).toBeInTheDocument();
  });

  it("persists manually entered API keys", async () => {
    localStorage.removeItem("oj_api_key");
    render(<App />);
    const credential = screen.getByLabelText("Provide API Key / Token");
    fireEvent.change(credential, { target: { value: "manual-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Use API Key" }));
    await waitFor(() => expect(localStorage.getItem("oj_api_key")).toBe("manual-key"));
  });

  it("does not persist restored OIDC tokens to localStorage", async () => {
    localStorage.removeItem("oj_api_key");
    authMock.restoreOIDCSession.mockResolvedValue("oidc-id-token");
    render(<App />);
    await waitFor(() => expect(localStorage.getItem("oj_api_key")).toBeNull());
  });

  it("stores local login sessions in sessionStorage and revokes them on sign out", async () => {
    localStorage.removeItem("oj_api_key");
    render(<App />);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "admin@example.test" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: "Log in with credentials" }));
    await waitFor(() => expect(sessionStorage.getItem("oj_session_token")).toBe("session-token"));
    expect(localStorage.getItem("oj_api_key")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(sessionStorage.getItem("oj_session_token")).toBeNull());
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/auth/logout"), expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({ Authorization: "Bearer session-token" }),
    }));
  });

  it("renders an empty suppressions response without crashing", async () => {
    window.location.hash = "suppressions";
    render(<App />);
    expect(await screen.findByText("No suppressed endpoints found.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Suppress endpoint" })).toBeInTheDocument();
  });

  it("creates an email with the visual template composer", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Templates" }));
    fireEvent.click(await screen.findByRole("button", { name: "+ New template" }));
    expect(screen.getByRole("tab", { name: "Visual composer", selected: true })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Friendly welcome" } });
    fireEvent.change(screen.getByLabelText("Headline"), { target: { value: "You’re in!" } });
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "We’re happy to welcome you." } });
    fireEvent.click(screen.getByRole("button", { name: "Save template" }));
    await waitFor(() => {
      const create = fetchMock.mock.calls.find(([url, init]) => String(url).includes("/v1/templates") && init?.method === "POST");
      expect(create).toBeTruthy();
      const payload = JSON.parse(String(create![1]?.body));
      expect(payload.html_template).toContain("data-openjourney-builder");
      expect(payload.html_template).toContain("You’re in!");
      expect(payload.html_template).toContain("We’re happy to welcome you.");
    });
  });
});
