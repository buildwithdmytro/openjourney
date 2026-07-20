import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import Button from "./Button";

afterEach(cleanup);

describe("Button", () => {
  it("renders as a button", () => {
    render(<Button>Click me</Button>);
    const button = screen.getByRole("button", { name: "Click me" });
    expect(button).toBeInTheDocument();
  });

  it("renders primary variant by default", () => {
    render(<Button>Primary</Button>);
    const button = screen.getByRole("button", { name: "Primary" });
    expect(button).toHaveClass("btn-primary");
  });

  it("renders secondary variant", () => {
    render(<Button variant="secondary">Secondary</Button>);
    const button = screen.getByRole("button", { name: "Secondary" });
    expect(button).toHaveClass("btn-secondary");
  });

  it("renders danger variant", () => {
    render(<Button variant="danger">Delete</Button>);
    const button = screen.getByRole("button", { name: "Delete" });
    expect(button).toHaveClass("btn-danger");
  });

  it("renders ghost variant", () => {
    render(<Button variant="ghost">Cancel</Button>);
    const button = screen.getByRole("button", { name: "Cancel" });
    expect(button).toHaveClass("btn-ghost");
  });

  it("handles click events", () => {
    const handleClick = vi.fn();
    render(<Button onClick={handleClick}>Click</Button>);
    fireEvent.click(screen.getByRole("button", { name: "Click" }));
    expect(handleClick).toHaveBeenCalled();
  });

  it("disables when disabled prop is true", () => {
    render(<Button disabled>Disabled</Button>);
    const button = screen.getByRole("button", { name: "Disabled" });
    expect(button).toBeDisabled();
  });

  it("disables when loading is true", () => {
    render(<Button loading>Loading</Button>);
    const button = screen.getByRole("button", { name: "Loading" });
    expect(button).toBeDisabled();
  });

  it("renders sm size", () => {
    render(<Button size="sm">Small</Button>);
    const button = screen.getByRole("button", { name: "Small" });
    expect(button).toHaveClass("btn-sm");
  });

  it("has focus-visible styling", () => {
    render(<Button>Focus</Button>);
    const button = screen.getByRole("button", { name: "Focus" });
    expect(button).toHaveClass("btn");
  });
});
