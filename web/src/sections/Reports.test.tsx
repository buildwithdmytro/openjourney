import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import Reports from "./Reports";

function response(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

const count = (total: number, unique = total) => ({ total, unique });
const funnel = {
  targeted: count(120, 100), sent: count(80, 70), suppressed: count(8, 8), no_consent: count(9, 9), fatigued: count(5, 5),
  render_failed: count(2, 2), send_failed: count(1, 1), failed: count(3, 3), holdout: count(12, 10),
  delivered: count(70, 60), opened: count(50, 40), clicked: count(30, 20), converted: count(20, 18),
};

beforeEach(() => {
  cleanup();
  window.location.hash = "#reports?type=campaign&id=campaign-1";
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/v1/campaigns")) return response([{ id: "campaign-1", name: "Welcome campaign" }]);
    if (url.endsWith("/v1/journeys")) return response({ journeys: [{ id: "journey-1", name: "Onboarding" }] });
    if (url.endsWith("/v1/experiments")) return response([{ id: "experiment-1", name: "CTA test" }]);
    if (url.endsWith("/v1/reports/campaigns/campaign-1")) return response({
      campaign_id: "campaign-1", funnel,
      deliverability: { bounced: count(10, 9), complained: count(2, 2), bounce_rate: 0.125, complaint_rate: 0.025 },
    });
    if (url.endsWith("/v1/reports/journeys/journey-1")) return response({
      journey_id: "journey-1", funnel,
      deliverability: { bounced: count(4, 4), complained: count(1, 1), bounce_rate: 0.05, complaint_rate: 0.0125 },
    });
    if (url.endsWith("/v1/reports/experiments/experiment-1")) return response({
      experiment_id: "experiment-1", winner_variant: "treatment", variants: [
        { label: "control", is_control: true, sent: 100, conversions: 10, rate: 0.1, uplift: 0, z_score: 0, p_value: 1, ci_low: 0, ci_high: 0, guardrails: [] },
        { label: "treatment", is_control: false, sent: 100, conversions: 20, rate: 0.2, uplift: 1, z_score: 1.98, p_value: 0.0477, ci_low: 0.001, ci_high: 0.199, guardrails: [] },
        { label: "alternate", is_control: false, sent: 90, conversions: 12, rate: 0.1333, uplift: 0.333, z_score: 0.8, p_value: 0.2, ci_low: -0.04, ci_high: 0.11, guardrails: [] },
      ],
    });
    throw new Error(`Unexpected request: ${url}`);
  }));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.location.hash = "";
});

it("renders API funnel and deliverability numbers in light and dark themes", async () => {
  const { container } = render(<Reports apiKey="key" baseURL="/api" />);
  const chart = await screen.findByRole("img", { name: "Delivery and conversion funnel" });
  expect(within(chart).getByText("120 total · 100 unique")).toBeInTheDocument();
  expect(within(chart).getByText("20 total · 18 unique")).toBeInTheDocument();
  expect(screen.getByText("12.5%")).toBeInTheDocument();
  expect(screen.getByText("10 bounced · 9 unique")).toBeInTheDocument();
  expect(container.querySelector(".reports-view")).toHaveAttribute("data-theme", "light");

  fireEvent.click(screen.getByRole("button", { name: "Use dark theme" }));
  expect(container.querySelector(".reports-view")).toHaveAttribute("data-theme", "dark");
  expect(screen.getByRole("img", { name: "Delivery and conversion funnel" })).toBeInTheDocument();
  expect(within(chart).getByText("70 total · 60 unique")).toBeInTheDocument();
});

it("shows experiment rates, uplift, p-values, and significance clearly", async () => {
  render(<Reports apiKey="key" baseURL="/api" />);
  await screen.findByRole("img", { name: "Delivery and conversion funnel" });
  fireEvent.change(screen.getByLabelText("Report type"), { target: { value: "experiment" } });

  const table = await screen.findByRole("table");
  const treatment = within(table).getByText("treatment").closest("tr");
  expect(treatment).not.toBeNull();
  expect(within(treatment!).getByText("20.0%")).toBeInTheDocument();
  expect(within(treatment!).getByText("100.0%")).toBeInTheDocument();
  expect(within(treatment!).getByText("0.0477")).toBeInTheDocument();
  expect(within(treatment!).getByText("Significant")).toBeInTheDocument();
  expect(within(table).getByText("Not yet significant")).toBeInTheDocument();
  expect(screen.getByText("Advisory winner: treatment")).toBeInTheDocument();
});
