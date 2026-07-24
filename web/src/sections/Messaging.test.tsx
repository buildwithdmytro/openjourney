import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Messaging from "./Messaging";

describe("Messaging section", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("creates an in-app message and refreshes the list", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({ id: "message-1", message_type: "card", status: "pending", rank: 0, categories: [], created_at: new Date().toISOString() })));
      }
      if (String(input).endsWith("/v1/templates")) return Promise.resolve(new Response(JSON.stringify({ templates: [] })));
      return Promise.resolve(new Response(JSON.stringify({ messages: [] })));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<Messaging apiKey="test" baseURL="http://localhost" />);
    fireEvent.click(screen.getByRole("button", { name: "Create message" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost/v1/messages",
      expect.objectContaining({ method: "POST", body: expect.stringContaining('"message_type":"card"') }),
    ));
    expect(await screen.findByRole("status")).toHaveTextContent("Message created.");
  });
});
