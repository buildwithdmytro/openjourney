import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Governance from "./Governance";

describe("Governance", () => {
  it("renders budget and activity and edits a provider without exposing secrets", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/v1/ai/providers")) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ providers: [{ id: "provider-1", provider: "fake", is_default: true, config: {}, endpoint_allowlist: [], monthly_budget_cents: 500, status: "active", created_at: "2026-01-01", updated_at: "2026-01-01" }] }) });
      if (url.endsWith("/v1/ai/budget")) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ usage: { period: "2026-07", cost_cents: 125, input_tokens: 10, output_tokens: 20 }, monthly_budget_cents: 500 }) });
      if (url.includes("/v1/ai/activity")) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ activities: [{ id: "activity-1", action: "ai.content_draft", provider: "fake", model: "fake-model", policy_decision: "allowed", cost_cents: 2, input_tokens: 4, output_tokens: 5, created_at: "2026-07-18T10:00:00Z" }] }) });
      if (url.includes("/v1/ai/field-classifications")) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ classifications: [] }) });
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<Governance apiKey="test-key" baseURL="/api" />);
    expect(await screen.findByText(/125¢ used of 500¢/)).toBeInTheDocument();
    expect(screen.getByText("ai.content_draft")).toBeInTheDocument();
    expect(screen.queryByText("oj_secret")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Monthly budget (cents)"), { target: { value: "750" } });
    fireEvent.click(screen.getByRole("button", { name: "Save provider" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/ai/providers/provider-1", expect.objectContaining({ method: "PUT", body: expect.stringContaining("750") })));
  });
});
