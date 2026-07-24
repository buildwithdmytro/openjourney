import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Extensions from "./Extensions";

describe("Extensions section", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("installs a signed extension manifest", async () => {
    const extension = { id: "extension-1", name: "Acme connector", publisher: "acme", latest_version: 1, status: "enabled" };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/v1/extensions/install")) return Promise.resolve(new Response(JSON.stringify({ extension })));
      if (String(input).endsWith("/v1/extensions")) return Promise.resolve(new Response(JSON.stringify({ extensions: [] })));
      if (String(input).includes("/config")) return Promise.resolve(new Response(JSON.stringify({ extension_id: "extension-1", config: {}, endpoint_allowlist: [], timeout_ms: 2000, max_memory_mb: 64, monthly_budget_cents: 0, rate_per_min: 600, status: "active" })));
      return Promise.resolve(new Response(JSON.stringify({ grants: [], activities: [], health: { state: "ok", consecutive_failures: 0 } })));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<Extensions apiKey="test" baseURL="http://localhost" />);
    fireEvent.change(screen.getByPlaceholderText(/Acme connector/), { target: { value: JSON.stringify({ name: "Acme connector", publisher: "acme", version: 1 }) } });
    fireEvent.click(screen.getByRole("button", { name: "Verify and install" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost/v1/extensions/install",
      expect.objectContaining({ method: "POST", body: expect.stringContaining('"publisher":"acme"') }),
    ));
    expect(await screen.findByRole("status")).toHaveTextContent("Signed extension installed and enabled.");
  });
});
