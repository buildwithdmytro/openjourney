import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import EmptyState from "./EmptyState";

afterEach(cleanup);

describe("EmptyState", () => {
  it("renders title", () => {
    render(<EmptyState title="No items" />);
    const title = screen.getByRole("heading", { level: 3, name: "No items" });
    expect(title).toBeInTheDocument();
  });

  it("renders description", () => {
    render(
      <EmptyState
        title="No items"
        description="Create a new item to get started."
      />
    );
    expect(screen.getByText("Create a new item to get started.")).toBeInTheDocument();
  });

  it("renders icon", () => {
    const { container } = render(
      <EmptyState
        title="No items"
        icon="plus"
      />
    );
    const svg = container.querySelector(".empty-state-icon svg");
    expect(svg).toBeInTheDocument();
  });

  it("renders CTA button with correct label and fires onClick", () => {
    const handleClick = vi.fn();
    render(
      <EmptyState
        title="No items"
        cta={{ label: "Add item", onClick: handleClick }}
      />
    );
    const button = screen.getByRole("button", { name: "Add item" });
    expect(button).toBeInTheDocument();
    fireEvent.click(button);
    expect(handleClick).toHaveBeenCalled();
  });

  it("renders without optional properties", () => {
    render(<EmptyState title="No items" />);
    const title = screen.getByRole("heading", { level: 3, name: "No items" });
    expect(title).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("renders with all properties", () => {
    const handleClick = vi.fn();
    render(
      <EmptyState
        title="No items"
        description="Get started by creating one."
        icon="info"
        cta={{ label: "Create", onClick: handleClick }}
      />
    );
    expect(screen.getByRole("heading", { level: 3, name: "No items" })).toBeInTheDocument();
    expect(screen.getByText("Get started by creating one.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create" })).toBeInTheDocument();
  });

  it("has empty-state class", () => {
    const { container } = render(<EmptyState title="No items" />);
    const div = container.firstChild as HTMLElement;
    expect(div).toHaveClass("empty-state");
  });
});
