import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import Toast from "./Toast";

describe("Toast", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  it("renders message with role=status", () => {
    const { container } = render(<Toast kind="success" message="Operation succeeded" />);
    const toast = container.querySelector('[role="status"]');
    expect(toast).toBeInTheDocument();
    expect(toast).toHaveTextContent("Operation succeeded");
  });

  it("renders with aria-live=polite", () => {
    const { container } = render(<Toast kind="success" message="Test" />);
    const toast = container.querySelector('[role="status"]');
    expect(toast).toHaveAttribute("aria-live", "polite");
  });

  it("renders close button with accessible name", () => {
    render(
      <Toast kind="success" message="Test" onDismiss={() => {}} />
    );
    const closeBtn = screen.getByRole("button", { name: /dismiss/i });
    expect(closeBtn).toBeInTheDocument();
  });

  it("calls onDismiss when close button is clicked", () => {
    const mockDismiss = vi.fn();
    const { container } = render(
      <Toast kind="success" message="Test" onDismiss={mockDismiss} />
    );
    const closeBtn = container.querySelector(".toast-close");
    fireEvent.click(closeBtn!);
    expect(mockDismiss).toHaveBeenCalledOnce();
  });

  it("auto-dismisses after duration", async () => {
    const mockDismiss = vi.fn();
    render(
      <Toast kind="success" message="Test" duration={3000} onDismiss={mockDismiss} />
    );
    vi.advanceTimersByTime(3000);
    expect(mockDismiss).toHaveBeenCalledOnce();
  });

  it("respects custom duration", () => {
    const mockDismiss = vi.fn();
    render(
      <Toast kind="error" message="Error" duration={1500} onDismiss={mockDismiss} />
    );
    vi.advanceTimersByTime(1400);
    expect(mockDismiss).not.toHaveBeenCalled();
    vi.advanceTimersByTime(100);
    expect(mockDismiss).toHaveBeenCalledOnce();
  });

  it("renders different kinds with appropriate class", () => {
    const { rerender, container } = render(<Toast kind="success" message="Success" />);
    let toast = container.querySelector('[role="status"]');
    expect(toast).toHaveClass("toast-success");

    rerender(<Toast kind="error" message="Error" />);
    toast = container.querySelector('[role="status"]');
    expect(toast).toHaveClass("toast-error");

    rerender(<Toast kind="warn" message="Warning" />);
    toast = container.querySelector('[role="status"]');
    expect(toast).toHaveClass("toast-warn");

    rerender(<Toast kind="info" message="Info" />);
    toast = container.querySelector('[role="status"]');
    expect(toast).toHaveClass("toast-info");
  });
});
