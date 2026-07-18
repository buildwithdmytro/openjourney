import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Copilots from "./Copilots";

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

    render(<Copilots apiKey="test-key" baseURL="/api" />);
    fireEvent.change(screen.getByLabelText("Describe the content"), { target: { value: "Welcome back" } });
    fireEvent.click(screen.getByRole("button", { name: "Create governed draft" }));

    await waitFor(() => expect(screen.getByText("Welcome back")).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/ai/copilots/content", expect.objectContaining({ method: "POST" }));
    expect(screen.getByText("Draft only")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Review & approve in templates" })).toBeInTheDocument();
  });
});
