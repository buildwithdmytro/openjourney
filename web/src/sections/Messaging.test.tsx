import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Messaging from "./Messaging";
import { ToastProvider } from "../components";

describe("Messaging section", () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => cleanup());

  it("creates an in-app message and refreshes the list", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({ id: "message-1", message_type: "card", status: "pending", rank: 0, categories: [], created_at: new Date().toISOString() })));
      }
      if (String(input).endsWith("/v1/templates")) return Promise.resolve(new Response(JSON.stringify({ templates: [] })));
      return Promise.resolve(new Response(JSON.stringify({ messages: [] })));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<ToastProvider><Messaging apiKey="test" baseURL="http://localhost" /></ToastProvider>);
    fireEvent.click(screen.getByRole("button", { name: "Create message" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost/v1/messages",
      expect.objectContaining({ method: "POST", body: expect.stringContaining('"message_type":"card"') }),
    ));
    expect(await screen.findByText("Message created.")).toBeInTheDocument();
    expect(screen.getByText("Message created successfully")).toBeInTheDocument();
  });

  it("disables message creation while the request is in flight", async () => {
    let release!: () => void;
    const pending = new Promise<Response>((resolve) => { release = () => resolve(new Response(JSON.stringify({ id: "message-1" }))); });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") return pending;
      if (String(input).endsWith("/v1/templates")) return Promise.resolve(new Response(JSON.stringify({ templates: [] })));
      return Promise.resolve(new Response(JSON.stringify({ messages: [] })));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<ToastProvider><Messaging apiKey="test" baseURL="http://localhost" /></ToastProvider>);
    const create = screen.getByRole("button", { name: "Create message" });
    fireEvent.click(create);
    await waitFor(() => expect(create).toBeDisabled());
    release();
    await waitFor(() => expect(create).not.toBeDisabled());
  });
});
