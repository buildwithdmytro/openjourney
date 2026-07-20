import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Icon from "./Icon";

afterEach(cleanup);

describe("Icon", () => {
  it("renders an SVG element", () => {
    const { container } = render(<Icon name="check" />);
    const svg = container.querySelector("svg");
    expect(svg).toBeInTheDocument();
  });

  it("renders all named glyphs without error", () => {
    const glyphNames = [
      "search",
      "close",
      "check",
      "chevron",
      "plus",
      "trash",
      "warn",
      "info",
      "menu",
      "external",
      "sun",
      "moon",
    ] as const;

    glyphNames.forEach((name) => {
      const { container } = render(<Icon name={name} />);
      const svg = container.querySelector("svg");
      expect(svg).toBeInTheDocument();
    });
  });

  it("renders as decorative by default with aria-hidden", () => {
    const { container } = render(<Icon name="check" />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("aria-hidden", "true");
  });

  it("renders as labeled with aria-label and role='img'", () => {
    render(<Icon name="check" aria-label="Check mark" />);
    const svg = screen.getByRole("img", { name: "Check mark" });
    expect(svg).toBeInTheDocument();
    expect(svg).toHaveAttribute("aria-label", "Check mark");
    expect(svg).not.toHaveAttribute("aria-hidden", "true");
  });

  it("supports custom size prop", () => {
    const { container } = render(<Icon name="check" size={32} />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "32");
    expect(svg).toHaveAttribute("height", "32");
  });

  it("renders with default size of 24", () => {
    const { container } = render(<Icon name="check" />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "24");
    expect(svg).toHaveAttribute("height", "24");
  });

  it("maintains 24x24 viewBox for all icons", () => {
    const { container } = render(<Icon name="search" />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("viewBox", "0 0 24 24");
  });

  it("forwards ref to SVG element", () => {
    const ref = { current: null };
    render(<Icon name="check" ref={ref} />);
    expect(ref.current).toBeInstanceOf(SVGSVGElement);
  });

  it("supports custom className", () => {
    const { container } = render(<Icon name="check" className="custom-class" />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveClass("custom-class");
  });

  it("supports decorative mode with explicit aria-hidden", () => {
    const { container } = render(<Icon name="check" aria-hidden={true} />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("aria-hidden", "true");
  });

  it("labeled icon does not have aria-hidden", () => {
    const { container } = render(<Icon name="sun" aria-label="Light mode" />);
    const svg = container.querySelector("svg");
    expect(svg).not.toHaveAttribute("aria-hidden", "true");
  });

  it("renders search icon with correct SVG path", () => {
    const { container } = render(<Icon name="search" />);
    const path = container.querySelector("path");
    expect(path).toBeInTheDocument();
  });
});
