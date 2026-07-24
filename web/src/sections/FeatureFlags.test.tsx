import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FeatureFlags from "./FeatureFlags";
import { ToastProvider } from "../components";

describe("FeatureFlags section", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("creates a flag through the editor", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({ id: "flag-1", key: "beta_checkout", environment: "production" })));
      }
      return Promise.resolve(new Response(JSON.stringify({ flags: [] })));
    });
    vi.stubGlobal("fetch", fetchMock);
    const modalRoot = document.createElement("div");
    modalRoot.id = "modal-root";
    document.body.appendChild(modalRoot);

    render(<ToastProvider><FeatureFlags apiKey="test" baseURL="http://localhost" /></ToastProvider>);
    await screen.findByText("No flags yet");
    fireEvent.click(screen.getByRole("button", { name: "New flag" }));
    fireEvent.change(screen.getByPlaceholderText("feature_flag_key"), { target: { value: "beta_checkout" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost/v1/flags",
      expect.objectContaining({ method: "POST", body: expect.stringContaining('"key":"beta_checkout"') }),
    ));
  });
});
