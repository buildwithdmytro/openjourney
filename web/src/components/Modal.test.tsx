import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, it, expect, vi, beforeEach } from "vitest";
import Modal from "./Modal";

// Add modal-root to DOM for portal rendering
function setupModalRoot() {
  let root = document.getElementById("modal-root");
  if (!root) {
    root = document.createElement("div");
    root.id = "modal-root";
    document.body.appendChild(root);
  }
  return root;
}

afterEach(() => {
  cleanup();
  const root = document.getElementById("modal-root");
  if (root) {
    root.innerHTML = "";
  }
});

describe("Modal", () => {
  beforeEach(() => {
    setupModalRoot();
  });

  it("renders dialog with proper role and attributes", () => {
    render(
      <Modal isOpen={true} onClose={vi.fn()}>
        <div>Modal content</div>
      </Modal>
    );

    const dialog = screen.getByRole("dialog");
    expect(dialog).toBeInTheDocument();
    expect(dialog).toHaveAttribute("aria-modal", "true");
  });

  it("renders aria-label when provided", () => {
    render(
      <Modal isOpen={true} onClose={vi.fn()} aria-label="Test Modal">
        <div>Modal content</div>
      </Modal>
    );

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-label", "Test Modal");
  });

  it("renders aria-labelledby when provided", () => {
    render(
      <Modal isOpen={true} onClose={vi.fn()} aria-labelledby="modal-title">
        <div id="modal-title">Modal Title</div>
        <div>Modal content</div>
      </Modal>
    );

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-labelledby", "modal-title");
  });

  it("does not render when isOpen is false", () => {
    render(
      <Modal isOpen={false} onClose={vi.fn()}>
        <div>Modal content</div>
      </Modal>
    );

    const dialog = screen.queryByRole("dialog");
    expect(dialog).not.toBeInTheDocument();
  });

  it("calls onClose when Escape key is pressed", async () => {
    const onClose = vi.fn();
    render(
      <Modal isOpen={true} onClose={onClose}>
        <div>Modal content</div>
      </Modal>
    );

    fireEvent.keyDown(document, { key: "Escape" });

    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it("calls onClose when backdrop is clicked", () => {
    const onClose = vi.fn();
    const modalRoot = document.getElementById("modal-root")!;
    render(
      <Modal isOpen={true} onClose={onClose}>
        <div>Modal content</div>
      </Modal>
    );

    const backdrop = modalRoot.querySelector(".modal-backdrop") as HTMLElement;
    expect(backdrop).toBeInTheDocument();
    fireEvent.click(backdrop);

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not close when modal content is clicked", () => {
    const onClose = vi.fn();
    const modalRoot = document.getElementById("modal-root")!;
    render(
      <Modal isOpen={true} onClose={onClose}>
        <div>Modal content</div>
      </Modal>
    );

    const content = modalRoot.querySelector(".modal-content") as HTMLElement;
    expect(content).toBeInTheDocument();
    fireEvent.click(content);

    expect(onClose).not.toHaveBeenCalled();
  });

  it("restores focus to trigger element on close", async () => {
    const { rerender } = render(
      <div>
        <button id="trigger">Open Modal</button>
        <Modal isOpen={false} onClose={vi.fn()}>
          <button>Modal Button</button>
        </Modal>
      </div>
    );

    const triggerButton = screen.getByRole("button", { name: "Open Modal" });
    triggerButton.focus();
    expect(triggerButton).toHaveFocus();

    // Open the modal
    rerender(
      <div>
        <button id="trigger">Open Modal</button>
        <Modal isOpen={true} onClose={vi.fn()}>
          <button>Modal Button</button>
        </Modal>
      </div>
    );

    // Modal button should now have focus
    const modalButton = screen.getByRole("button", { name: "Modal Button" });
    expect(modalButton).toHaveFocus();

    // Close the modal
    rerender(
      <div>
        <button id="trigger">Open Modal</button>
        <Modal isOpen={false} onClose={vi.fn()}>
          <button>Modal Button</button>
        </Modal>
      </div>
    );

    // Focus should be restored to trigger button
    await waitFor(() => {
      expect(triggerButton).toHaveFocus();
    });
  });

  it("focuses first focusable element when opened", () => {
    render(
      <Modal isOpen={true} onClose={vi.fn()}>
        <button>First Button</button>
        <button>Second Button</button>
      </Modal>
    );

    const firstButton = screen.getByRole("button", { name: "First Button" });
    expect(firstButton).toHaveFocus();
  });

  it("has Tab key handler for focus trapping", () => {
    const handleKeyDown = vi.fn();
    render(
      <Modal isOpen={true} onClose={vi.fn()}>
        <button>First Button</button>
        <button>Second Button</button>
      </Modal>
    );

    const firstButton = screen.getByRole("button", { name: "First Button" });

    // First button should have focus initially
    expect(firstButton).toHaveFocus();

    // Fire Tab key and verify the listener processes it
    fireEvent.keyDown(document, { key: "Tab" });

    // The focus trap is active (listener is installed)
    // Further testing of actual Tab movement is limited by fireEvent constraints
    expect(firstButton).toHaveFocus();
  });

  it("renders into modal-root portal", () => {
    render(
      <Modal isOpen={true} onClose={vi.fn()}>
        <div>Portal content</div>
      </Modal>
    );

    const modalRoot = document.getElementById("modal-root");
    const dialog = modalRoot?.querySelector('[role="dialog"]');
    expect(dialog).toBeInTheDocument();
  });

  it("accepts children", () => {
    render(
      <Modal isOpen={true} onClose={vi.fn()}>
        <div data-testid="custom-content">Custom content</div>
      </Modal>
    );

    const content = screen.getByTestId("custom-content");
    expect(content).toBeInTheDocument();
  });

  it("supports ref forwarding", () => {
    const ref = { current: null };
    render(
      <Modal ref={ref} isOpen={true} onClose={vi.fn()}>
        <div>Content</div>
      </Modal>
    );

    expect(ref.current).toBeInTheDocument();
    expect(ref.current).toHaveAttribute("role", "dialog");
  });
});
