import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Skeleton from "./Skeleton";

afterEach(cleanup);

describe("Skeleton", () => {
  it("renders as a div", () => {
    render(<Skeleton data-testid="skeleton" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toBeInTheDocument();
  });

  it("has skeleton class", () => {
    render(<Skeleton data-testid="skeleton" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveClass("skeleton");
  });

  it("renders with default height", () => {
    render(<Skeleton data-testid="skeleton" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveStyle("height: 20px");
  });

  it("renders with default width", () => {
    render(<Skeleton data-testid="skeleton" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveStyle("width: 100%");
  });

  it("renders with custom width", () => {
    render(<Skeleton data-testid="skeleton" width="200px" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveStyle("width: 200px");
  });

  it("renders with custom height", () => {
    render(<Skeleton data-testid="skeleton" height="40px" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveStyle("height: 40px");
  });

  it("renders as circle when circle prop is true", () => {
    render(<Skeleton data-testid="skeleton" circle />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveStyle("border-radius: 50%");
  });

  it("renders with custom className", () => {
    render(<Skeleton data-testid="skeleton" className="custom-class" />);
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveClass("custom-class");
  });

  it("respects inline style", () => {
    render(
      <Skeleton
        data-testid="skeleton"
        style={{ backgroundColor: "rgb(255, 0, 0)" }}
      />
    );
    const skeleton = screen.getByTestId("skeleton");
    expect(skeleton).toHaveStyle("background-color: rgb(255, 0, 0)");
  });
});
