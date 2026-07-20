import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ErrorState from "./ErrorState";

describe("ErrorState", () => {
  it("renders title and description", () => {
    render(
      <ErrorState
        title="Something went wrong"
        description="Failed to load data"
      />
    );
    expect(screen.getByRole("heading", { name: /something went wrong/i })).toBeInTheDocument();
    expect(screen.getByText("Failed to load data")).toBeInTheDocument();
  });

  it("renders default title when not provided", () => {
    render(<ErrorState description="An error occurred" />);
    expect(screen.getByRole("heading", { name: /error/i })).toBeInTheDocument();
  });

  it("renders icon by default", () => {
    render(<ErrorState description="Test error" />);
    const icon = document.querySelector(".error-state-icon");
    expect(icon).toBeInTheDocument();
  });

  it("renders retry button when onRetry is provided", () => {
    const mockRetry = vi.fn();
    render(
      <ErrorState
        description="Error occurred"
        onRetry={mockRetry}
      />
    );
    const retryBtn = screen.getByRole("button", { name: /retry/i });
    expect(retryBtn).toBeInTheDocument();
    fireEvent.click(retryBtn);
    expect(mockRetry).toHaveBeenCalledOnce();
  });

  it("does not render retry button when onRetry is not provided", () => {
    const { container } = render(<ErrorState description="Error occurred" />);
    const retryButtons = container.querySelectorAll(".error-state-cta");
    expect(retryButtons.length).toBe(0);
  });

  it("renders with custom icon", () => {
    render(
      <ErrorState
        icon="close"
        description="Custom icon error"
      />
    );
    const iconDiv = document.querySelector(".error-state-icon");
    expect(iconDiv).toBeInTheDocument();
  });
});
