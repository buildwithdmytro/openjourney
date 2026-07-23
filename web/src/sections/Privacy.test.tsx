import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Privacy from "./Privacy";

describe("Privacy console", () => {
  it("verifies a request and only enables export download after completion", async () => {
    const fetchMock = vi.fn((input: RequestInfo, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/v1/privacy/requests") && init?.method === "POST") return Promise.resolve(new Response(JSON.stringify({ id: "req-1", external_id: "customer-1", request_type: "export", status: "pending", verification_status: "unverified", verification_token: "secret", created_at: "2026-01-01" }), { status: 202 }));
      if (url.endsWith("/verify")) return Promise.resolve(new Response(JSON.stringify({ id: "req-1", external_id: "customer-1", request_type: "export", status: "completed", verification_status: "verified", created_at: "2026-01-01" }), { status: 200 }));
      if (url.endsWith("/download")) return Promise.resolve(new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } }));
      return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("URL", { ...URL, createObjectURL: vi.fn(() => "blob:test"), revokeObjectURL: vi.fn() });
    render(<Privacy apiKey="token" baseURL="/api" />);
    fireEvent.change(screen.getByLabelText("External ID"), { target: { value: "customer-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Submit privacy request" }));
    await screen.findByText("export · pending");
    expect(screen.getByRole("button", { name: "Download export" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Requester verification token"), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Verify and process" }));
    await waitFor(() => expect(screen.getByText("export · completed")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Download export" })).not.toBeDisabled();
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/privacy/requests/req-1/verify", expect.objectContaining({ method: "POST" }));
  });
});
