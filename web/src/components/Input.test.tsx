import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Input from "./Input";

afterEach(cleanup);

describe("Input", () => {
  it("renders an input element", () => {
    render(<Input type="text" />);
    const input = screen.getByRole("textbox");
    expect(input).toBeInTheDocument();
  });

  it("forwards ref", () => {
    const ref = { current: null };
    render(<Input ref={ref} type="text" />);
    expect(ref.current).toBeInstanceOf(HTMLInputElement);
  });

  it("sets aria-invalid when error prop is provided", () => {
    render(<Input error="This field is required" />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute("aria-invalid", "true");
  });

  it("does not set aria-invalid when no error", () => {
    render(<Input />);
    const input = screen.getByRole("textbox");
    expect(input).not.toHaveAttribute("aria-invalid");
  });

  it("passes through other attributes", async () => {
    render(<Input type="email" placeholder="Enter email" required />);
    const input = screen.getByPlaceholderText("Enter email");
    expect(input).toHaveAttribute("type", "email");
    expect(input).toHaveAttribute("required");
  });

  it("has focus-visible styling", () => {
    render(<Input />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveClass("input");
  });
});
