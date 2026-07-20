import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Spinner from "./Spinner";

afterEach(cleanup);

describe("Spinner", () => {
  it("renders as a status role element", () => {
    render(<Spinner />);
    const spinner = screen.getByRole("status");
    expect(spinner).toBeInTheDocument();
  });

  it("renders with default label", () => {
    render(<Spinner />);
    const spinner = screen.getByRole("status", { name: "Loading" });
    expect(spinner).toBeInTheDocument();
  });

  it("renders with custom label", () => {
    render(<Spinner label="Loading data…" />);
    const spinner = screen.getByRole("status", { name: "Loading data…" });
    expect(spinner).toBeInTheDocument();
  });

  it("renders sm size", () => {
    render(<Spinner size="sm" />);
    const spinner = screen.getByRole("status");
    expect(spinner).toHaveClass("spinner-sm");
  });

  it("renders md size", () => {
    render(<Spinner size="md" />);
    const spinner = screen.getByRole("status");
    expect(spinner).toHaveClass("spinner-md");
  });

  it("renders lg size", () => {
    render(<Spinner size="lg" />);
    const spinner = screen.getByRole("status");
    expect(spinner).toHaveClass("spinner-lg");
  });

  it("has spinner class", () => {
    render(<Spinner />);
    const spinner = screen.getByRole("status");
    expect(spinner).toHaveClass("spinner");
  });
});
