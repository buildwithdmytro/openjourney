import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import Catalogs from "./Catalogs";

describe("Catalogs section", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("switches tabs and creates a connected-content source", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        return new Response(JSON.stringify({ id: "source-1", name: "Inventory API", status: "draft", enabled: false }));
      }
      if (String(input).endsWith("/v1/connected-content-sources")) return new Response(JSON.stringify({ sources: [] }));
      return new Response(JSON.stringify({ catalogs: [] }));
    });

    globalThis.fetch = fetchMock;

    render(<Catalogs apiKey="test" baseURL="http://localhost" />);

    expect(screen.getByText("Catalogs & connected content")).toBeInTheDocument();
    expect(screen.getByText("Reference data")).toBeInTheDocument();
    expect(screen.getByText(/allowlisted, authed external data fetchers/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Connected Content" }));
    fireEvent.click(screen.getByRole("button", { name: "New source" }));
    fireEvent.change(screen.getByPlaceholderText("API source name"), { target: { value: "Inventory API" } });
    fireEvent.change(screen.getByPlaceholderText("api.example.com"), { target: { value: "inventory.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Save source" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost/v1/connected-content-sources",
      expect.objectContaining({ method: "POST", body: expect.stringContaining('"name":"Inventory API"') }),
    ));
    expect(await screen.findByRole("status")).toHaveTextContent("Source saved.");
  });
});
