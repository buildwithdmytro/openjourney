import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Access from "./Access";

describe("Access console", () => {
  it("loads the catalog and provisions a team", async () => {
    const fetchMock = vi.fn((input: RequestInfo, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/v1/roles")) return Promise.resolve(new Response(JSON.stringify({ roles: [] }), { status: 200 }));
      if (url.endsWith("/v1/users")) return Promise.resolve(new Response(JSON.stringify({ users: [] }), { status: 200 }));
      if (url.endsWith("/v1/teams")) return Promise.resolve(new Response(JSON.stringify({ teams: [] }), { status: 200 }));
      if (url.endsWith("/v1/permissions")) return Promise.resolve(new Response(JSON.stringify({ permissions: [{ key: "teams:write", resource: "teams", verb: "write", description: "Manage teams", system: true, created_at: "2026-01-01" }] }), { status: 200 }));
      return Promise.resolve(new Response(JSON.stringify({ id: "team-1", name: "Support", member_ids: [], role_ids: [] }), { status: 201 }));
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<Access apiKey="token" baseURL="/api" />);
    await screen.findByText("teams:write");
    fireEvent.change(screen.getByLabelText("Team name"), { target: { value: "Support" } });
    fireEvent.click(screen.getByRole("button", { name: "Create team" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/teams", expect.objectContaining({ method: "POST" })));
  });
});
