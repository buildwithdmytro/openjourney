import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import Acquisition from "./Acquisition";
import { ToastProvider } from "../components";

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
    render(<ToastProvider><Acquisition apiKey="key" baseURL="/api" /></ToastProvider>);
    fireEvent.click(screen.getByRole("button", { name: "New form" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Newsletter" } });
    fireEvent.change(screen.getByLabelText("Field 1 key"), { target: { value: "email" } });
    fireEvent.change(screen.getByLabelText("Field 1 type"), { target: { value: "email" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/forms"), expect.objectContaining({ method: "POST" })));
    // The publish button is gated on the saved draft id; wait for it to enable so
    // the click cannot race the setForm(saved) re-render (flaky under suite load).
    await waitFor(() => expect((screen.getByRole("button", { name: "Publish version" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/forms/form-1/publish"), expect.objectContaining({ method: "POST" })));
    expect(screen.getByText("Form published successfully")).toBeInTheDocument();
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
    render(<ToastProvider><Acquisition apiKey="key" baseURL="/api" /></ToastProvider>);
    fireEvent.click(screen.getByRole("button", { name: "Pages" }));
    fireEvent.click(screen.getByRole("button", { name: "New page" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Launch" } });
    fireEvent.change(screen.getByLabelText("Public slug"), { target: { value: "launch" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/pages"), expect.objectContaining({ method: "POST" })));
    // The publish button is gated on the saved draft id; wait for it to enable so
    // the click cannot race the setPage(saved) re-render (flaky under suite load).
    await waitFor(() => expect((screen.getByRole("button", { name: "Publish version" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Publish version" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/pages/page-1/publish"), expect.objectContaining({ method: "POST" })));
  });
});
