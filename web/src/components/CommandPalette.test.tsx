import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { CommandPalette } from "./CommandPalette";

beforeEach(() => {
  const modalRoot = document.createElement("div");
  modalRoot.id = "modal-root";
  document.body.appendChild(modalRoot);
});

afterEach(() => {
  cleanup();
  const modalRoot = document.getElementById("modal-root");
  if (modalRoot) {
    modalRoot.remove();
  }
});

describe("CommandPalette", () => {
  it("renders when open", () => {
    render(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    expect(screen.getByPlaceholderText(/search views/i)).toBeInTheDocument();
    expect(screen.getByRole("listbox")).toBeInTheDocument();
  });

  it("does not render when closed", () => {
    const { container } = render(
      <CommandPalette
        isOpen={false}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    expect(container.firstChild).toBeNull();
  });

  it("filters items based on query", () => {
    render(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);
    fireEvent.change(input, { target: { value: "campaign" } });

    expect(screen.getByText("Campaigns")).toBeInTheDocument();
    expect(screen.queryByText("Profiles")).not.toBeInTheDocument();
  });

  it("shows empty state when no results", () => {
    render(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);
    fireEvent.change(input, { target: { value: "nonexistent" } });

    expect(screen.getByText("No views found")).toBeInTheDocument();
  });

  it("navigates with arrow keys", async () => {
    const onNavigate = vi.fn();
    render(
      <CommandPalette
        isOpen={true}
        onClose={onNavigate}
        onNavigate={onNavigate}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);
    input.focus();

    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowUp" });

    const items = screen.getAllByRole("option");
    expect(items[0]).toHaveAttribute("aria-selected", "false");
    expect(items[1]).toHaveAttribute("aria-selected", "true");
  });

  it("selects item with Enter key", async () => {
    const onNavigate = vi.fn();
    const onClose = vi.fn();
    render(
      <CommandPalette
        isOpen={true}
        onClose={onClose}
        onNavigate={onNavigate}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);
    input.focus();

    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onNavigate).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("closes on Escape key", () => {
    const onClose = vi.fn();
    render(
      <CommandPalette
        isOpen={true}
        onClose={onClose}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);
    fireEvent.keyDown(input, { key: "Escape" });

    expect(onClose).toHaveBeenCalled();
  });

  it("navigates when clicking an item", () => {
    const onNavigate = vi.fn();
    const onClose = vi.fn();
    render(
      <CommandPalette
        isOpen={true}
        onClose={onClose}
        onNavigate={onNavigate}
        currentView="profiles"
      />
    );

    const campaignItem = screen.getByText("Campaigns").closest(".command-palette-item");
    fireEvent.click(campaignItem!);

    expect(onNavigate).toHaveBeenCalledWith("campaigns");
    expect(onClose).toHaveBeenCalled();
  });

  it("shows categories for items", () => {
    render(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const items = screen.getAllByRole("option");
    expect(items.length).toBeGreaterThan(0);

    const profileItem = screen.getByText("Profiles");
    expect(profileItem).toBeInTheDocument();
  });

  it("wraps around when navigating past the end", () => {
    render(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);
    input.focus();

    const items = screen.getAllByRole("option");
    const itemCount = items.length;

    fireEvent.keyDown(input, { key: "ArrowUp" });

    expect(items[itemCount - 1]).toHaveAttribute("aria-selected", "true");
  });

  it("focuses input when opened", async () => {
    const { rerender } = render(
      <CommandPalette
        isOpen={false}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    rerender(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search views/i)).toHaveFocus();
    });
  });

  it("clears query when opened", () => {
    const { rerender } = render(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "campaign" } });

    expect(input.value).toBe("campaign");

    rerender(
      <CommandPalette
        isOpen={false}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    rerender(
      <CommandPalette
        isOpen={true}
        onClose={vi.fn()}
        onNavigate={vi.fn()}
        currentView="profiles"
      />
    );

    const newInput = screen.getByPlaceholderText(/search views/i) as HTMLInputElement;
    expect(newInput.value).toBe("");
  });

  it("drives entirely by keyboard", () => {
    const onNavigate = vi.fn();
    const onClose = vi.fn();
    render(
      <CommandPalette
        isOpen={true}
        onClose={onClose}
        onNavigate={onNavigate}
        currentView="profiles"
      />
    );

    const input = screen.getByPlaceholderText(/search views/i);

    fireEvent.change(input, { target: { value: "camp" } });
    expect(screen.getByText("Campaigns")).toBeInTheDocument();

    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onNavigate).toHaveBeenCalledWith("campaigns");
    expect(onClose).toHaveBeenCalled();
  });
});
