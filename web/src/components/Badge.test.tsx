import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Badge from "./Badge";

afterEach(cleanup);

describe("Badge", () => {
  it("renders as a span with default kind", () => {
    render(<Badge>Draft</Badge>);
    const badge = screen.getByText("Draft");
    expect(badge).toHaveClass("pill");
  });

  it("renders with danger kind (default)", () => {
    render(<Badge kind="danger">Danger</Badge>);
    const badge = screen.getByText("Danger");
    expect(badge).toHaveClass("pill");
  });

  it("renders with success kind", () => {
    render(<Badge kind="success">Active</Badge>);
    const badge = screen.getByText("Active");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("subscribed");
  });

  it("renders with draft kind", () => {
    render(<Badge kind="draft">Draft</Badge>);
    const badge = screen.getByText("Draft");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("draft");
  });

  it("renders with published kind", () => {
    render(<Badge kind="published">Published</Badge>);
    const badge = screen.getByText("Published");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("published");
  });

  it("renders with active kind", () => {
    render(<Badge kind="active">Active</Badge>);
    const badge = screen.getByText("Active");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("published");
  });

  it("renders with completed kind", () => {
    render(<Badge kind="completed">Completed</Badge>);
    const badge = screen.getByText("Completed");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("published");
  });

  it("renders with paused kind", () => {
    render(<Badge kind="paused">Paused</Badge>);
    const badge = screen.getByText("Paused");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("waiting");
  });

  it("renders with waiting kind", () => {
    render(<Badge kind="waiting">Waiting</Badge>);
    const badge = screen.getByText("Waiting");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("waiting");
  });

  it("renders with subscribed kind", () => {
    render(<Badge kind="subscribed">Subscribed</Badge>);
    const badge = screen.getByText("Subscribed");
    expect(badge).toHaveClass("pill");
    expect(badge).toHaveClass("subscribed");
  });

  it("forwards ref", () => {
    const ref = React.createRef<HTMLSpanElement>();
    render(
      <Badge ref={ref} kind="success">
        Test
      </Badge>
    );
    expect(ref.current).toBeInstanceOf(HTMLSpanElement);
  });

  it("applies custom className", () => {
    render(
      <Badge kind="success" className="custom">
        Custom
      </Badge>
    );
    const badge = screen.getByText("Custom");
    expect(badge).toHaveClass("custom");
  });
});
