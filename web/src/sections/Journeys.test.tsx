import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import Journeys from "./Journeys";

const mockApiKey = "test-key";

describe("Journeys", () => {
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
    vi.unstubAllGlobals();
  });
  it("renders the journeys list on desktop viewport", () => {
    // Mock matchMedia to return false (desktop viewport)
    vi.stubGlobal(
      "matchMedia",
      vi.fn().mockImplementation(() => ({
        matches: false,
        media: "(max-width: 760px)",
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    );

    render(<Journeys apiKey={mockApiKey} />);

    // The message should not be displayed on desktop
    expect(screen.queryByText(/best viewed on a larger screen/i)).not.toBeInTheDocument();
  });
});
