import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import AuditViewer from "./AuditViewer";
import * as api from "../api";

vi.mock("../api", () => ({
  listAuditEvents: vi.fn(),
  verifyAuditChain: vi.fn(),
}));

describe("AuditViewer section", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => {
    cleanup();
  });

  it("fetches audit events and chain verification status on mount", async () => {
    vi.mocked(api.listAuditEvents).mockResolvedValue([
      {
        id: "evt-1",
        seq: 1,
        occurred_at: "2026-01-01T00:00:00Z",
        actor_type: "user",
        actor_id: "usr-1",
        action: "role.create",
        resource_type: "role",
        resource_id: "role-1",
        metadata: {},
        row_hash: "hash1234567890",
      },
    ]);
    vi.mocked(api.verifyAuditChain).mockResolvedValue({
      status: "ok",
      intact: true,
      total_events: 1,
    });

    render(<AuditViewer apiKey="test-key" baseURL="http://localhost" />);

    await waitFor(() => expect(screen.getByText("Chain Intact")).toBeInTheDocument());
    expect(screen.getByText("role.create")).toBeInTheDocument();
    expect(screen.getByText("user:usr-1")).toBeInTheDocument();
  });

  it("filters audit events by actor_id and resource_type", async () => {
    vi.mocked(api.listAuditEvents).mockResolvedValue([]);
    vi.mocked(api.verifyAuditChain).mockResolvedValue({
      status: "ok",
      intact: true,
      total_events: 0,
    });

    render(<AuditViewer apiKey="test-key" baseURL="http://localhost" />);

    await waitFor(() => expect(api.listAuditEvents).toHaveBeenCalledWith("http://localhost", "test-key", undefined));

    fireEvent.change(screen.getByLabelText("Actor ID"), { target: { value: "usr-99" } });
    fireEvent.change(screen.getByLabelText("Resource type"), { target: { value: "team" } });
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));

    await waitFor(() =>
      expect(api.listAuditEvents).toHaveBeenCalledWith("http://localhost", "test-key", {
        actor_id: "usr-99",
        resource_type: "team",
      })
    );
  });
});
