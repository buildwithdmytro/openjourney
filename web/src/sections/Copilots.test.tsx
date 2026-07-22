import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToastProvider } from "../components";
import Copilots from "./Copilots";

afterEach(() => {
  cleanup();
});

describe("Copilots", () => {
  it("round-trips a content draft and keeps approval in the human editor", async () => {
    const fetchMock = vi.fn(() => Promise.resolve({
      ok: true,
      status: 201,
      json: () => Promise.resolve({
        draft: { id: "template-draft-1", subject_template: "Welcome back", html_template: "Hello!", status: "draft" },
        activity_id: "activity-1",
      }),
    }));
    vi.stubGlobal("fetch", fetchMock);

    render(
      <ToastProvider>
        <Copilots apiKey="test-key" baseURL="/api" />
      </ToastProvider>
    );
    fireEvent.change(screen.getByLabelText("Describe the content"), { target: { value: "Welcome back" } });
    fireEvent.click(screen.getByRole("button", { name: "Create governed draft" }));

    await waitFor(() => expect(screen.getByText("Welcome back")).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/ai/copilots/content", expect.objectContaining({ method: "POST" }));
    expect(screen.getByText("Draft only")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Review & approve in templates" })).toBeInTheDocument();
  });

  it("supports inline draft review with accept and refine actions", async () => {
    const fetchMock = vi.fn(() => Promise.resolve({
      ok: true,
      status: 201,
      json: () => Promise.resolve({
        draft: { id: "template-draft-1", subject_template: "Special Offer", html_template: "Save 20%!", status: "draft" },
        activity_id: "activity-1",
      }),
    }));
    vi.stubGlobal("fetch", fetchMock);

    render(
      <ToastProvider>
        <Copilots apiKey="test-key" baseURL="/api" />
      </ToastProvider>
    );

    fireEvent.change(screen.getByLabelText("Describe the content"), { target: { value: "Special offer" } });
    fireEvent.click(screen.getByRole("button", { name: "Create governed draft" }));

    await waitFor(() => expect(screen.getByText("Special Offer")).toBeInTheDocument());

    // Verify inline Accept action
    const acceptBtn = screen.getByRole("button", { name: "Accept Draft" });
    expect(acceptBtn).toBeInTheDocument();
    fireEvent.click(acceptBtn);

    await waitFor(() => expect(screen.getByRole("button", { name: "Accepted" })).toBeInTheDocument());

    // Verify inline Refine action
    const refineBtn = screen.getByRole("button", { name: "Refine Draft" });
    expect(refineBtn).toBeInTheDocument();
    fireEvent.click(refineBtn);
  });
});
