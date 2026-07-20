import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { ToastProvider, useToast } from "./ToastProvider";

function TestComponent() {
  const { push } = useToast();
  return (
    <div>
      <button onClick={() => push({ kind: "success", message: "Success!" })}>
        Push Success
      </button>
      <button onClick={() => push({ kind: "error", message: "Error!" })}>
        Push Error
      </button>
      <button onClick={() => push({ kind: "info", message: "Info!", duration: 1000 })}>
        Push Info
      </button>
    </div>
  );
}

describe("ToastProvider", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  it("throws when useToast is used outside provider", () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    expect(() => {
      render(<TestComponent />);
    }).toThrow("useToast must be used within a ToastProvider");

    consoleErrorSpy.mockRestore();
  });

  it("renders toasts in a container with proper role", () => {
    const { container } = render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );
    const region = container.querySelector('[role="region"][aria-label="Notifications"]');
    expect(region).toBeInTheDocument();
    expect(region).toHaveClass("toast-container");
  });

  it("pushes and displays a toast", () => {
    const { container } = render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );
    const button = container.querySelector("button");
    fireEvent.click(button!);

    const toast = container.querySelector('[role="status"]');
    expect(toast).toHaveTextContent("Success!");
  });

  it("displays multiple toasts", () => {
    const { container } = render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );

    const buttons = container.querySelectorAll("button");
    fireEvent.click(buttons[0]);
    fireEvent.click(buttons[1]);

    const toasts = container.querySelectorAll('[role="status"]');
    expect(toasts).toHaveLength(2);
    expect(toasts[0]).toHaveTextContent("Success!");
    expect(toasts[1]).toHaveTextContent("Error!");
  });

  it("removes toast when dismiss button is clicked", () => {
    const { container } = render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );

    const buttons = container.querySelectorAll("button");
    fireEvent.click(buttons[0]);

    let toast = container.querySelector('[role="status"]');
    expect(toast).toBeInTheDocument();

    const dismissButton = container.querySelector(".toast-close");
    fireEvent.click(dismissButton!);

    expect(container.querySelector('[role="status"]')).not.toBeInTheDocument();
  });

  it("passes duration through to Toast component", () => {
    const { container } = render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );

    const buttons = container.querySelectorAll("button");
    fireEvent.click(buttons[2]); // Push Info with duration: 1000

    const toast = container.querySelector('[role="status"]');
    expect(toast).toBeInTheDocument();
    expect(toast).toHaveClass("toast-info");
  });

  it("can push multiple toasts and dismiss individually", () => {
    const { container } = render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );

    const buttons = container.querySelectorAll("button");
    fireEvent.click(buttons[0]);
    fireEvent.click(buttons[1]);

    let toasts = container.querySelectorAll('[role="status"]');
    expect(toasts).toHaveLength(2);

    const dismissButtons = container.querySelectorAll(".toast-close");
    fireEvent.click(dismissButtons[0]);

    toasts = container.querySelectorAll('[role="status"]');
    expect(toasts).toHaveLength(1);
    expect(toasts[0]).toHaveTextContent("Error!");
  });
});
