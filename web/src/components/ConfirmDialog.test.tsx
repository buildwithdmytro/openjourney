import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, it, expect, vi, beforeEach } from "vitest";
import ConfirmDialog from "./ConfirmDialog";

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

describe("ConfirmDialog", () => {
  beforeEach(() => {
    setupModalRoot();
  });

  it("renders dialog with title and message", () => {
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    expect(screen.getByText("Delete item?")).toBeInTheDocument();
    expect(screen.getByText("This action cannot be undone.")).toBeInTheDocument();
  });

  it("does not render when isOpen is false", () => {
    render(
      <ConfirmDialog
        isOpen={false}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    expect(screen.queryByText("Delete item?")).not.toBeInTheDocument();
  });

  it("renders confirm and cancel buttons with default text", () => {
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Confirm" })).toBeInTheDocument();
  });

  it("renders confirm and cancel buttons with custom text", () => {
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
        confirmText="Delete"
        cancelText="Keep"
      />
    );

    expect(screen.getByRole("button", { name: "Keep" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  });

  it("calls onClose when cancel button is clicked", () => {
    const onClose = vi.fn();
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={onClose}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    const cancelButton = screen.getByRole("button", { name: "Cancel" });
    fireEvent.click(cancelButton);

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onConfirm when confirm button is clicked", async () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={onClose}
        onConfirm={onConfirm}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    const confirmButton = screen.getByRole("button", { name: "Confirm" });
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(onConfirm).toHaveBeenCalledTimes(1);
    });

    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it("handles async onConfirm", async () => {
    const onConfirm = vi.fn(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
    });
    const onClose = vi.fn();
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={onClose}
        onConfirm={onConfirm}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    const confirmButton = screen.getByRole("button", { name: "Confirm" });
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(onConfirm).toHaveBeenCalledTimes(1);
    });

    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it("calls onClose when Escape key is pressed", async () => {
    const onClose = vi.fn();
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={onClose}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    fireEvent.keyDown(document, { key: "Escape" });

    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it("renders with aria-labelledby pointing to title", () => {
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-labelledby", "confirm-dialog-title");
    const title = screen.getByText("Delete item?");
    expect(title).toHaveAttribute("id", "confirm-dialog-title");
  });

  it("disables buttons while confirming", async () => {
    const onConfirm = vi.fn(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={onConfirm}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    const confirmButton = screen.getByRole("button", { name: "Confirm" });
    const cancelButton = screen.getByRole("button", { name: "Cancel" });

    fireEvent.click(confirmButton);

    // Both buttons should be disabled during async operation
    await waitFor(() => {
      expect(confirmButton).toBeDisabled();
      expect(cancelButton).toBeDisabled();
    });
  });

  it("supports ref forwarding", () => {
    const ref = { current: null };
    render(
      <ConfirmDialog
        ref={ref}
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    expect(ref.current).toBeInTheDocument();
    expect(ref.current).toHaveAttribute("role", "dialog");
  });

  it("shows danger variant for dangerous actions", () => {
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Delete item?"
        message="This action cannot be undone."
        isDangerous={true}
        confirmText="Delete"
      />
    );

    const confirmButton = screen.getByRole("button", { name: "Delete" });
    expect(confirmButton).toHaveClass("btn-danger");
  });

  it("shows primary variant for non-dangerous actions", () => {
    render(
      <ConfirmDialog
        isOpen={true}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        title="Are you sure?"
        message="This will proceed."
        isDangerous={false}
      />
    );

    const confirmButton = screen.getByRole("button", { name: "Confirm" });
    expect(confirmButton).toHaveClass("btn-primary");
  });

  it("still calls onClose even if onConfirm throws", async () => {
    const onConfirm = vi.fn(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
      throw new Error("Operation failed");
    });
    const onClose = vi.fn();

    // Suppress console errors for this test
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    render(
      <ConfirmDialog
        isOpen={true}
        onClose={onClose}
        onConfirm={onConfirm}
        title="Delete item?"
        message="This action cannot be undone."
      />
    );

    const confirmButton = screen.getByRole("button", { name: "Confirm" });
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(onConfirm).toHaveBeenCalledTimes(1);
    });

    // onClose should be called even if onConfirm throws
    // because we catch the error in handleConfirm
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    consoleErrorSpy.mockRestore();
  });
});
