import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Textarea from "./Textarea";

afterEach(cleanup);

describe("Textarea", () => {
  it("renders a textarea element", () => {
    render(<Textarea />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toBeInTheDocument();
  });

  it("forwards ref", () => {
    const ref = { current: null };
    render(<Textarea ref={ref} />);
    expect(ref.current).toBeInstanceOf(HTMLTextAreaElement);
  });

  it("sets aria-invalid when error prop is provided", () => {
    render(<Textarea error="This field is required" />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveAttribute("aria-invalid", "true");
  });

  it("does not set aria-invalid when no error", () => {
    render(<Textarea />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).not.toHaveAttribute("aria-invalid");
  });

  it("passes through other attributes", () => {
    render(<Textarea placeholder="Enter text" required />);
    const textarea = screen.getByPlaceholderText("Enter text");
    expect(textarea).toHaveAttribute("required");
  });

  it("has focus-visible styling", () => {
    render(<Textarea />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveClass("textarea");
  });
});
