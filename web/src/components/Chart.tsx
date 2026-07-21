import React from "react";

export interface ChartSeries {
  label: string;
  data: number[];
}

export interface LineChartProps extends React.HTMLAttributes<SVGSVGElement> {
  series: ChartSeries[];
  xLabels?: string[];
  height?: number;
}

export interface BarChartProps extends React.HTMLAttributes<SVGSVGElement> {
  series: ChartSeries[];
  xLabels?: string[];
  height?: number;
}

export interface FunnelChartProps extends React.HTMLAttributes<SVGSVGElement> {
  stages: Array<{ label: string; total: number; unique: number }>;
}

export interface SparklineProps extends React.HTMLAttributes<SVGSVGElement> {
  data: number[];
  label: string;
  height?: number;
}

const LineChart = React.forwardRef<SVGSVGElement, LineChartProps>(
  ({ series, xLabels, height = 200, className, ...props }, ref) => {
    if (!series.length || !series[0].data.length) return null;

    const width = 400;
    const padding = { top: 20, right: 20, bottom: 40, left: 40 };
    const plotWidth = width - padding.left - padding.right;
    const plotHeight = height - padding.top - padding.bottom;

    const dataPoints = series[0].data.length;
    const max = Math.max(...series.flatMap((s) => s.data), 1);
    const min = Math.min(...series.flatMap((s) => s.data), 0);
    const range = max - min || 1;

    const colors = ["currentColor"];

    return (
      <svg
        ref={ref}
        viewBox={`0 0 ${width} ${height}`}
        className={`chart line-chart ${className || ""}`.trim()}
        {...props}
      >
        {series.map((s, seriesIndex) => {
          const points = s.data
            .map((value, i) => ({
              x: padding.left + (i / (dataPoints - 1 || 1)) * plotWidth,
              y: padding.top + plotHeight - ((value - min) / range) * plotHeight,
            }))
            .map((p) => `${p.x},${p.y}`)
            .join(" ");

          return (
            <polyline
              key={seriesIndex}
              points={points}
              fill="none"
              stroke={colors[seriesIndex % colors.length]}
              strokeWidth="2"
              vectorEffect="non-scaling-stroke"
            />
          );
        })}

        {xLabels && xLabels.length > 0 && (
          <g opacity={0.5}>
            {xLabels.map((label, i) => (
              <text
                key={i}
                x={padding.left + (i / (dataPoints - 1 || 1)) * plotWidth}
                y={height - padding.bottom + 20}
                textAnchor="middle"
                fontSize="12"
                fill="currentColor"
              >
                {label}
              </text>
            ))}
          </g>
        )}
      </svg>
    );
  }
);
LineChart.displayName = "LineChart";

const BarChart = React.forwardRef<SVGSVGElement, BarChartProps>(
  ({ series, xLabels, height = 200, className, ...props }, ref) => {
    if (!series.length || !series[0].data.length) return null;

    const width = 400;
    const padding = { top: 20, right: 20, bottom: 40, left: 40 };
    const plotWidth = width - padding.left - padding.right;
    const plotHeight = height - padding.top - padding.bottom;

    const dataPoints = series[0].data.length;
    const barWidth = plotWidth / dataPoints / (series.length + 1);
    const max = Math.max(...series.flatMap((s) => s.data), 1);
    const min = Math.min(...series.flatMap((s) => s.data), 0);
    const range = max - min || 1;

    const colors = ["currentColor"];

    return (
      <svg
        ref={ref}
        viewBox={`0 0 ${width} ${height}`}
        className={`chart bar-chart ${className || ""}`.trim()}
        {...props}
      >
        {series.map((s, seriesIndex) => {
          return s.data.map((value, i) => {
            const barHeight = ((value - min) / range) * plotHeight;
            const x =
              padding.left +
              (i / dataPoints) * plotWidth +
              ((seriesIndex + 1) / (series.length + 1)) * (plotWidth / dataPoints);
            const y = padding.top + plotHeight - barHeight;

            return (
              <rect
                key={`${seriesIndex}-${i}`}
                x={x}
                y={y}
                width={barWidth}
                height={barHeight}
                fill={colors[seriesIndex % colors.length]}
              />
            );
          });
        })}

        {xLabels && xLabels.length > 0 && (
          <g opacity={0.5}>
            {xLabels.map((label, i) => (
              <text
                key={i}
                x={padding.left + ((i + 0.5) / dataPoints) * plotWidth}
                y={height - padding.bottom + 20}
                textAnchor="middle"
                fontSize="12"
                fill="currentColor"
              >
                {label}
              </text>
            ))}
          </g>
        )}
      </svg>
    );
  }
);
BarChart.displayName = "BarChart";

const FunnelChart = React.forwardRef<SVGSVGElement, FunnelChartProps>(
  ({ stages, className, ...props }, ref) => {
    if (!stages.length) return null;

    const width = 760;
    const height = 330;
    const maximum = Math.max(...stages.map((s) => s.total), 1);

    return (
      <svg
        ref={ref}
        viewBox={`0 0 ${width} ${height}`}
        className={`chart funnel-chart ${className || ""}`.trim()}
        role="img"
        aria-label="Delivery and conversion funnel"
        {...props}
      >
        <title>Delivery and conversion funnel, using total and unique counts from the report</title>
        {stages.map((stage, index) => {
          const barWidth = Math.max(4, (stage.total / maximum) * 490);
          const y = 12 + index * 52;

          return (
            <g key={index}>
              <text className="funnel-label" x="0" y={y + 22} fill="currentColor">
                {stage.label}
              </text>
              <rect className="funnel-track" x="112" y={y} width="500" height="32" rx="7" />
              <rect className="funnel-bar" x="112" y={y} width={barWidth} height="32" rx="7" />
              <text className="funnel-value" x="625" y={y + 21} fill="currentColor">
                {stage.total} total · {stage.unique} unique
              </text>
            </g>
          );
        })}
      </svg>
    );
  }
);
FunnelChart.displayName = "FunnelChart";

const Sparkline = React.forwardRef<SVGSVGElement, SparklineProps>(
  ({ data, label, height = 32, className, ...props }, ref) => {
    if (!data.length) return null;

    const width = 120;
    const max = Math.max(...data, 1);
    const min = Math.min(...data, 0);
    const range = max - min || 1;

    const points = data
      .map((value, i) => ({
        x: (i / (data.length - 1 || 1)) * width,
        y: height - ((value - min) / range) * (height - 4) - 2,
      }))
      .map((p) => `${p.x},${p.y}`)
      .join(" ");

    return (
      <svg
        ref={ref}
        viewBox={`0 0 ${width} ${height}`}
        className={`chart sparkline ${className || ""}`.trim()}
        role="img"
        aria-label={label}
        {...props}
      >
        <polyline
          points={points}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
    );
  }
);
Sparkline.displayName = "Sparkline";

export { LineChart, BarChart, FunnelChart, Sparkline };
