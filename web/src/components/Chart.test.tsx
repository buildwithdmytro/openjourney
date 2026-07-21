import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LineChart, BarChart, FunnelChart, Sparkline } from "./Chart";

describe("LineChart", () => {
  it("renders SVG with polyline for series data", () => {
    const { container } = render(
      <LineChart
        series={[{ label: "Series 1", data: [10, 20, 30] }]}
        xLabels={["Jan", "Feb", "Mar"]}
      />
    );
    const svg = container.querySelector("svg.line-chart");
    expect(svg).toBeTruthy();
    const polylines = container.querySelectorAll("polyline");
    expect(polylines.length).toBe(1);
  });

  it("renders multiple series with different colors", () => {
    const { container } = render(
      <LineChart
        series={[
          { label: "Series 1", data: [10, 20, 30] },
          { label: "Series 2", data: [15, 25, 35] },
        ]}
      />
    );
    const polylines = container.querySelectorAll("polyline");
    expect(polylines.length).toBe(2);
  });

  it("renders x-axis labels", () => {
    const { container } = render(
      <LineChart
        series={[{ label: "Series 1", data: [10, 20, 30] }]}
        xLabels={["Jan", "Feb", "Mar"]}
      />
    );
    const textElements = container.querySelectorAll("text");
    const labels = Array.from(textElements).filter((el) =>
      ["Jan", "Feb", "Mar"].includes(el.textContent || "")
    );
    expect(labels.length).toBe(3);
  });

  it("returns null for empty series", () => {
    const { container } = render(<LineChart series={[]} />);
    const svg = container.querySelector("svg");
    expect(svg).toBeFalsy();
  });

  it("returns null for series with empty data", () => {
    const { container } = render(
      <LineChart series={[{ label: "Empty", data: [] }]} />
    );
    const svg = container.querySelector("svg");
    expect(svg).toBeFalsy();
  });

  it("respects height prop", () => {
    const { container } = render(
      <LineChart
        series={[{ label: "Series 1", data: [10, 20, 30] }]}
        height={300}
      />
    );
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("viewBox")).toContain("300");
  });

  it("applies custom className", () => {
    const { container } = render(
      <LineChart
        series={[{ label: "Series 1", data: [10, 20, 30] }]}
        className="custom-class"
      />
    );
    const svg = container.querySelector("svg");
    expect(svg?.className.baseVal).toContain("custom-class");
  });
});

describe("BarChart", () => {
  it("renders SVG with rectangles for series data", () => {
    const { container } = render(
      <BarChart
        series={[{ label: "Series 1", data: [10, 20, 30] }]}
        xLabels={["Jan", "Feb", "Mar"]}
      />
    );
    const svg = container.querySelector("svg.bar-chart");
    expect(svg).toBeTruthy();
    const rectangles = container.querySelectorAll("rect");
    expect(rectangles.length).toBeGreaterThan(0);
  });

  it("renders multiple series with grouped bars", () => {
    const { container } = render(
      <BarChart
        series={[
          { label: "Series 1", data: [10, 20, 30] },
          { label: "Series 2", data: [15, 25, 35] },
        ]}
      />
    );
    const rectangles = container.querySelectorAll("rect");
    expect(rectangles.length).toBe(6);
  });

  it("renders x-axis labels", () => {
    const { container } = render(
      <BarChart
        series={[{ label: "Series 1", data: [10, 20, 30] }]}
        xLabels={["A", "B", "C"]}
      />
    );
    const textElements = container.querySelectorAll("text");
    const labels = Array.from(textElements).filter((el) =>
      ["A", "B", "C"].includes(el.textContent || "")
    );
    expect(labels.length).toBe(3);
  });

  it("returns null for empty series", () => {
    const { container } = render(<BarChart series={[]} />);
    const svg = container.querySelector("svg");
    expect(svg).toBeFalsy();
  });

  it("returns null for series with empty data", () => {
    const { container } = render(
      <BarChart series={[{ label: "Empty", data: [] }]} />
    );
    const svg = container.querySelector("svg");
    expect(svg).toBeFalsy();
  });
});

describe("FunnelChart", () => {
  it("renders SVG with funnel stages", () => {
    const { container } = render(
      <FunnelChart
        stages={[
          { label: "Stage 1", total: 100, unique: 90 },
          { label: "Stage 2", total: 50, unique: 45 },
        ]}
      />
    );
    const svg = container.querySelector("svg.funnel-chart");
    expect(svg).toBeTruthy();
  });

  it("has accessible role and aria-label", () => {
    const { container } = render(
      <FunnelChart
        stages={[
          { label: "Stage 1", total: 100, unique: 90 },
          { label: "Stage 2", total: 50, unique: 45 },
        ]}
      />
    );
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("role")).toBe("img");
    expect(svg?.getAttribute("aria-label")).toBe(
      "Delivery and conversion funnel"
    );
  });

  it("renders stage labels and values", () => {
    const { container } = render(
      <FunnelChart
        stages={[
          { label: "Sent", total: 1000, unique: 950 },
          { label: "Opened", total: 500, unique: 450 },
        ]}
      />
    );
    const textElements = container.querySelectorAll("text");
    const textContent = Array.from(textElements)
      .map((el) => el.textContent)
      .join(" ");
    expect(textContent).toContain("Sent");
    expect(textContent).toContain("Opened");
    expect(textContent).toContain("1000 total");
    expect(textContent).toContain("950 unique");
  });

  it("renders bars with proper widths relative to values", () => {
    const { container } = render(
      <FunnelChart
        stages={[
          { label: "Stage 1", total: 100, unique: 90 },
          { label: "Stage 2", total: 50, unique: 45 },
        ]}
      />
    );
    const bars = container.querySelectorAll(".funnel-bar");
    expect(bars.length).toBe(2);
    const bar1Width = parseFloat(bars[0].getAttribute("width") || "0");
    const bar2Width = parseFloat(bars[1].getAttribute("width") || "0");
    expect(bar1Width).toBeGreaterThan(bar2Width);
  });

  it("returns null for empty stages", () => {
    const { container } = render(<FunnelChart stages={[]} />);
    const svg = container.querySelector("svg");
    expect(svg).toBeFalsy();
  });
});

describe("Sparkline", () => {
  it("renders SVG with polyline for data", () => {
    const { container } = render(
      <Sparkline data={[10, 20, 30, 25, 15]} label="Trend" />
    );
    const svg = container.querySelector("svg.sparkline");
    expect(svg).toBeTruthy();
    const polyline = container.querySelector("polyline");
    expect(polyline).toBeTruthy();
  });

  it("has accessible role and aria-label", () => {
    const { container } = render(
      <Sparkline data={[10, 20, 30, 25, 15]} label="Revenue Trend" />
    );
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("role")).toBe("img");
    expect(svg?.getAttribute("aria-label")).toBe("Revenue Trend");
  });

  it("renders polyline with proper stroke properties", () => {
    const { container } = render(
      <Sparkline data={[10, 20, 30, 25, 15]} label="Trend" />
    );
    const polyline = container.querySelector("polyline");
    expect(polyline?.getAttribute("stroke")).toBe("currentColor");
    expect(polyline?.getAttribute("fill")).toBe("none");
  });

  it("returns null for empty data", () => {
    const { container } = render(<Sparkline data={[]} label="Trend" />);
    const svg = container.querySelector("svg");
    expect(svg).toBeFalsy();
  });

  it("respects height prop", () => {
    const { container } = render(
      <Sparkline data={[10, 20, 30, 25, 15]} label="Trend" height={48} />
    );
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("viewBox")).toContain("48");
  });

  it("applies custom className", () => {
    const { container } = render(
      <Sparkline
        data={[10, 20, 30, 25, 15]}
        label="Trend"
        className="custom-sparkline"
      />
    );
    const svg = container.querySelector("svg");
    expect(svg?.className.baseVal).toContain("custom-sparkline");
  });
});
