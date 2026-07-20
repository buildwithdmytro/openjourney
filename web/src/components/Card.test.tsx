import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Card from "./Card";

afterEach(cleanup);

describe("Card", () => {
  it("renders as a div with card class", () => {
    render(<Card data-testid="card">Content</Card>);
    const card = screen.getByTestId("card");
    expect(card).toHaveClass("card");
    expect(card.tagName).toBe("DIV");
  });

  it("renders as an article when variant is article", () => {
    render(
      <Card variant="article" data-testid="card">
        Content
      </Card>
    );
    const card = screen.getByTestId("card");
    expect(card).toHaveClass("card");
    expect(card.tagName).toBe("ARTICLE");
  });

  it("applies custom className", () => {
    render(
      <Card className="custom-class" data-testid="card">
        Content
      </Card>
    );
    const card = screen.getByTestId("card");
    expect(card).toHaveClass("card");
    expect(card).toHaveClass("custom-class");
  });

  it("forwards ref", () => {
    const ref = React.createRef<HTMLDivElement>();
    render(<Card ref={ref}>Content</Card>);
    expect(ref.current).toBeInstanceOf(HTMLDivElement);
  });

  it("accepts children", () => {
    render(
      <Card data-testid="card">
        <h2>Title</h2>
        <p>Description</p>
      </Card>
    );
    const card = screen.getByTestId("card");
    expect(card).toHaveTextContent("Title");
    expect(card).toHaveTextContent("Description");
  });
});
