import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import Acquisition from "./Acquisition";

const response = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));
afterEach(() => cleanup());

describe("Acquisition", () => {
  it("round-trips a form draft and publishes it", async () => {
    const saved = { id: "form-1", name: "Newsletter", status: "draft", draft: { fields: [{ key: "email", type: "email", required: true, consent: true }] }, latest_version: 0 };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input); const method = init?.method || "GET";
      if (url.endsWith("/v1/forms") && method === "GET") return response({ forms: [] });
      if (url.endsWith("/v1/pages") && method === "GET") return response({ pages: [] });
      if (url.endsWith("/v1/assets") && method === "GET") return response({ assets: [] });
      if (url.endsWith("/v1/forms") && method === "POST") return response(saved, 201);
      if (url.endsWith("/v1/forms/form-1/publish")) return response({ version: 1 }, 201);
      throw new Error(`Unexpected request: ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<Acquisition apiKey="key" baseURL="/api" />);
    fireEvent.click(screen.getByRole("button", { name: "New form" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Newsletter" } });
    fireEvent.change(screen.getByLabelText("Field 1 key"), { target: { value: "email" } });
    fireEvent.change(screen.getByLabelText("Field 1 type"), { target: { value: "email" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/forms"), expect.objectContaining({ method: "POST" })));
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/forms/form-1/publish"), expect.objectContaining({ method: "POST" })));
  });

  it("round-trips a landing page draft and publishes it", async () => {
    const saved = { id: "page-1", name: "Launch", slug: "launch", status: "draft", draft: { template: "<h1>Launch</h1>" }, latest_version: 0 };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input); const method = init?.method || "GET";
      if (url.endsWith("/v1/forms") && method === "GET") return response({ forms: [] });
      if (url.endsWith("/v1/pages") && method === "GET") return response({ pages: [] });
      if (url.endsWith("/v1/assets") && method === "GET") return response({ assets: [] });
      if (url.endsWith("/v1/pages") && method === "POST") return response(saved, 201);
      if (url.endsWith("/v1/pages/page-1/publish")) return response({ version: 1 }, 201);
      throw new Error(`Unexpected request: ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<Acquisition apiKey="key" baseURL="/api" />);
    fireEvent.click(screen.getByRole("button", { name: "Pages" }));
    fireEvent.click(screen.getByRole("button", { name: "New page" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Launch" } });
    fireEvent.change(screen.getByLabelText("Public slug"), { target: { value: "launch" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/pages"), expect.objectContaining({ method: "POST" })));
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/pages/page-1/publish"), expect.objectContaining({ method: "POST" })));
  });
});
