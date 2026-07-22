import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import Catalogs from "./Catalogs";

describe("Catalogs section", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the catalogs section with description and tabs", () => {
    const fetchMock = vi.fn(async () => {
      return {
        ok: true,
        json: async () => ({}),
      } as Response;
    });

    globalThis.fetch = fetchMock;

    render(<Catalogs apiKey="test" baseURL="http://localhost" />);

    expect(screen.getByText("Catalogs & connected content")).toBeInTheDocument();
    expect(screen.getByText("Reference data")).toBeInTheDocument();
    expect(screen.getByText(/allowlisted, authed external data fetchers/)).toBeInTheDocument();
  });
});
