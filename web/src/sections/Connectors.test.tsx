import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Connectors from "./Connectors";

const pipeline = { id: "pipe-1", tenant_id: "t", workspace_id: "w", app_id: "a", connector_extension_id: "ext-1", name: "Warehouse source", direction: "source", status: "draft", schedule_enabled: false, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" };
const run = { id: "run-1", pipeline_id: "pipe-1", job_type: "warehouse.sync", status: "succeeded", rows_in: 4, rows_out: 4, rows_rejected: 0, started_at: "2026-01-01T01:00:00Z" };

describe("Connectors", () => {
  it("lists, creates, and shows governed pipeline runs", async () => {
    const fetchMock = vi.fn((input: RequestInfo, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/v1/connectors/pipelines") && init?.method === "POST") return Promise.resolve({ ok: true, status: 201, json: () => Promise.resolve(pipeline) });
      if (url.endsWith("/v1/connectors/pipelines")) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ pipelines: [pipeline] }) });
      if (url.includes("/runs")) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ runs: [run] }) });
      if (url.includes("/identity/identify")) return Promise.resolve({ ok: true, status: 202, json: () => Promise.resolve({}) });
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<Connectors apiKey="key" baseURL="/api" />);
    await screen.findByText("Warehouse source");
    fireEvent.click(screen.getByText("Warehouse source"));
    await screen.findByText("succeeded");
    fireEvent.click(screen.getByRole("button", { name: "New pipeline" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New source" } });
    fireEvent.change(screen.getByLabelText("Connector extension ID"), { target: { value: "ext-2" } });
    fireEvent.click(screen.getByRole("button", { name: "Create pipeline" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/connectors/pipelines", expect.objectContaining({ method: "POST" })));
    fireEvent.change(screen.getByLabelText("Profile external ID"), { target: { value: "customer-1" } });
    fireEvent.change(screen.getByLabelText("Value"), { target: { value: "person@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Emit identify" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/identity/identify", expect.objectContaining({ method: "POST" })));
  });
});
