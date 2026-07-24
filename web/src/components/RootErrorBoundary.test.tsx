import { fireEvent, render, screen } from "@testing-library/react";
import { Component, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import RootErrorBoundary from "./RootErrorBoundary";

class ThrowOnRender extends Component<{ children?: ReactNode }> {
  render(): ReactNode {
    throw new Error("pre-auth chrome failed");
  }
}

describe("RootErrorBoundary", () => {
  it("renders recovery UI when the pre-auth chrome throws", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    render(
      <RootErrorBoundary>
        <ThrowOnRender />
      </RootErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("OpenJourney could not load");
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(consoleError).toHaveBeenCalled();
    consoleError.mockRestore();
  });
});
