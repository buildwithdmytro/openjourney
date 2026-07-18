import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Scoring from "./Scoring";

const response = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));

describe("Scoring", () => {
  it("creates a versioned model and inspects profile scores", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input); const method = init?.method || "GET";
      if (url.endsWith("/v1/scoring/models") && method === "GET") return response({ models: [] });
      if (url.endsWith("/v1/scoring/models") && method === "POST") return response({ id: "model-1", name: "Purchase propensity", kind: "expression", latest_version: 0 }, 201);
      if (url.endsWith("/v1/scoring/models/model-1/versions")) return response({ id: "version-1", version: 1 }, 201);
      if (url.endsWith("/v1/scoring/profiles/profile-1")) return response({ scores: [{ profile_id: "profile-1", scoring_model_id: "model-1", score_name: "purchase_propensity", value: 0.8, model_version: 1, computed_at: "2026-01-01T00:00:00Z" }] });
      throw new Error(`Unexpected request: ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<Scoring apiKey="key" baseURL="/api" />);
    fireEvent.change(screen.getByLabelText("Scoring model name"), { target: { value: "Purchase propensity" } });
    fireEvent.click(screen.getByRole("button", { name: "Create draft version" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/scoring/models/model-1/versions"), expect.objectContaining({ method: "POST" })));

    fireEvent.change(screen.getByLabelText("Profile ID"), { target: { value: "profile-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Inspect scores" }));
    expect(await screen.findByText("purchase_propensity")).toBeInTheDocument();
    expect(screen.getByText("0.8")).toBeInTheDocument();
  });
});
