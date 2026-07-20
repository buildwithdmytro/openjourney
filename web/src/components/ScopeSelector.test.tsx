import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import ScopeSelector from "./ScopeSelector";

afterEach(() => {
  cleanup();
});

const AVAILABLE_SCOPES = [
  "profiles:read",
  "profiles:write",
  "segments:read",
  "segments:write",
];

describe("ScopeSelector combobox", () => {
  it("renders button with combobox ARIA attributes", () => {
    render(
      <ScopeSelector
        selected={[]}
        onChange={vi.fn()}
        availableScopes={AVAILABLE_SCOPES}
      />
    );

    const button = screen.getByRole("button");
    expect(button).toHaveAttribute("aria-haspopup", "listbox");
    expect(button).toHaveAttribute("aria-expanded", "false");
  });

  it("toggles aria-expanded when opened and closed", async () => {
    render(
      <ScopeSelector
        selected={[]}
        onChange={vi.fn()}
        availableScopes={AVAILABLE_SCOPES}
      />
    );

    const button = screen.getByRole("button");
    expect(button).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(button);

    await waitFor(() => {
      expect(button).toHaveAttribute("aria-expanded", "true");
    });

    fireEvent.click(button);

    await waitFor(() => {
      expect(button).toHaveAttribute("aria-expanded", "false");
    });
  });

  it("renders listbox with option roles when opened", async () => {
    render(
      <ScopeSelector
        selected={[]}
        onChange={vi.fn()}
        availableScopes={AVAILABLE_SCOPES}
      />
    );

    const button = screen.getByRole("button");
    fireEvent.click(button);

    await waitFor(() => {
      const listbox = screen.getByRole("listbox");
      expect(listbox).toBeInTheDocument();
      const options = screen.getAllByRole("option");
      expect(options).toHaveLength(AVAILABLE_SCOPES.length);
    });
  });

  it("closes on Escape key", async () => {
    render(
      <ScopeSelector
        selected={[]}
        onChange={vi.fn()}
        availableScopes={AVAILABLE_SCOPES}
      />
    );

    const button = screen.getByRole("button");
    fireEvent.click(button);

    await waitFor(() => {
      expect(button).toHaveAttribute("aria-expanded", "true");
    });

    fireEvent.keyDown(document, { key: "Escape" });

    await waitFor(() => {
      expect(button).toHaveAttribute("aria-expanded", "false");
    });
  });

  it("navigates options with arrow keys", async () => {
    render(
      <ScopeSelector
        selected={[]}
        onChange={vi.fn()}
        availableScopes={AVAILABLE_SCOPES}
      />
    );

    const button = screen.getByRole("button");
    fireEvent.click(button);

    const options = screen.getAllByRole("option");
    expect(options[0]).toHaveAttribute("aria-selected", "false");
    expect(options[0]).toHaveClass("focused");

    fireEvent.keyDown(options[0], { key: "ArrowDown" });

    await waitFor(() => {
      expect(options[0]).not.toHaveClass("focused");
      expect(options[1]).toHaveClass("focused");
    });

    fireEvent.keyDown(options[1], { key: "ArrowUp" });

    await waitFor(() => {
      expect(options[0]).toHaveClass("focused");
      expect(options[1]).not.toHaveClass("focused");
    });
  });

  it("sets aria-selected on checked options", async () => {
    const onChange = vi.fn();
    render(
      <ScopeSelector
        selected={["profiles:read"]}
        onChange={onChange}
        availableScopes={AVAILABLE_SCOPES}
      />
    );

    const button = screen.getByRole("button");
    fireEvent.click(button);

    const options = screen.getAllByRole("option");
    const selectedOption = options.find(
      (opt) => opt.textContent?.includes("profiles:read")
    );

    expect(selectedOption).toHaveAttribute("aria-selected", "true");
  });
});
