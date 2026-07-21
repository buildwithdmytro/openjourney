import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Analytics from "./Analytics";

function response(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    })
  );
}

const mockCampaign = { id: "campaign-1", name: "Welcome Campaign", workspace_id: "workspace-1" };
const mockJourney = { id: "journey-1", name: "Onboarding", workspace_id: "workspace-1" };
const mockSavedReport = { id: "report-1", name: "Q1 Report", report_type: "funnel", query: { granularity: "day" } };

const mockFunnelReport = {
  buckets: [
    {
      time: "2024-01-01T00:00:00Z",
      funnel: {
        sent: { total: 100, unique: 90 },
        delivered: { total: 95, unique: 85 },
        opened: { total: 50, unique: 45 },
        clicked: { total: 20, unique: 18 },
        converted: { total: 10, unique: 9 },
      },
    },
    {
      time: "2024-01-02T00:00:00Z",
      funnel: {
        sent: { total: 120, unique: 110 },
        delivered: { total: 115, unique: 105 },
        opened: { total: 60, unique: 55 },
        clicked: { total: 25, unique: 23 },
        converted: { total: 15, unique: 14 },
      },
    },
  ],
};

const mockRetentionReport = {
  cohorts: [
    {
      cohort_time: "2024-01-01T00:00:00Z",
      sizes: [100, 90, 80, 70],
    },
    {
      cohort_time: "2024-01-02T00:00:00Z",
      sizes: [110, 100, 95],
    },
  ],
};

const mockGrowthReport = {
  buckets: [
    {
      time: "2024-01-01T00:00:00Z",
      new_profiles: 50,
      net_growth: 30,
      segment_memberships: 200,
    },
    {
      time: "2024-01-02T00:00:00Z",
      new_profiles: 60,
      net_growth: 40,
      segment_memberships: 240,
    },
  ],
};

const mockCostReport = {
  buckets: [
    {
      time: "2024-01-01T00:00:00Z",
      send_count: 100,
    },
    {
      time: "2024-01-02T00:00:00Z",
      send_count: 120,
    },
  ],
};

function createFetchMock(overrides?: Record<string, (url: string, method: string) => Promise<Response> | undefined>) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method || "GET";

    // Check for custom overrides first
    if (overrides) {
      for (const [key, handler] of Object.entries(overrides)) {
        if (url.includes(key)) {
          const result = handler(url, method);
          if (result) return result;
        }
      }
    }

    // Default behavior
    if (url.includes("/v1/campaigns")) return response([mockCampaign]);
    if (url.includes("/v1/journeys")) return response({ journeys: [mockJourney] });
    if (url.includes("/v1/saved-reports")) return response({ reports: [mockSavedReport] });

    if (url.includes("/reports/campaigns/campaign-1/funnel-over-time")) return response(mockFunnelReport);
    if (url.includes("/reports/campaigns/campaign-1/retention")) return response(mockRetentionReport);
    if (url.includes("/reports/campaigns/campaign-1/growth")) return response(mockGrowthReport);
    if (url.includes("/reports/campaigns/campaign-1/cost")) return response(mockCostReport);

    if (url.includes("/reports/journeys/journey-1/funnel-over-time")) return response(mockFunnelReport);
    if (url.includes("/reports/journeys/journey-1/retention")) return response(mockRetentionReport);
    if (url.includes("/reports/journeys/journey-1/growth")) return response(mockGrowthReport);
    if (url.includes("/reports/journeys/journey-1/cost")) return response(mockCostReport);

    throw new Error(`Unexpected request: ${method} ${url}`);
  });
}

beforeEach(() => {
  cleanup();
  vi.stubGlobal("fetch", createFetchMock());
  vi.stubGlobal("prompt", vi.fn(() => "New Report"));
  vi.stubGlobal("confirm", vi.fn(() => true));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("Analytics", () => {
  it("renders over-time funnel chart from campaign report", async () => {
    render(<Analytics apiKey="key" baseURL="/api" />);

    const funnelChart = await screen.findByText("Funnel over time");
    expect(funnelChart).toBeInTheDocument();

    const performanceCard = screen.getByText("Funnel over time").closest("div");
    expect(performanceCard).toBeInTheDocument();
  });

  it("renders retention cohort matrix from campaign report", async () => {
    render(<Analytics apiKey="key" baseURL="/api" />);

    const retentionHeading = await screen.findByText("Retention by cohort");
    expect(retentionHeading).toBeInTheDocument();

    const table = screen.getByRole("table");
    expect(within(table).getByText("Cohort")).toBeInTheDocument();
    expect(within(table).getByText("+0")).toBeInTheDocument();
  });

  it("renders cost chart from campaign report", async () => {
    render(<Analytics apiKey="key" baseURL="/api" />);

    const costHeading = await screen.findByText("Cost per period");
    expect(costHeading).toBeInTheDocument();
  });

  it("renders growth trends chart from campaign report", async () => {
    render(<Analytics apiKey="key" baseURL="/api" />);

    const growthHeading = await screen.findByText("Growth trends");
    expect(growthHeading).toBeInTheDocument();
  });

  it("saves a new report", async () => {
    const fetchMock = createFetchMock({
      "/v1/saved-reports": (url: string, method: string) => {
        if (method === "POST") {
          return response({ id: "report-2", name: "New Report", report_type: "funnel", query: {} }, 201);
        }
        return response({ reports: [mockSavedReport] });
      },
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<Analytics apiKey="key" baseURL="/api" />);

    await screen.findByText("Funnel over time");

    const saveButton = screen.getByRole("button", { name: "Save Report" });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/v1/saved-reports"),
        expect.objectContaining({ method: "POST" })
      );
    });
  });

  it("loads and displays saved reports", async () => {
    render(<Analytics apiKey="key" baseURL="/api" />);

    const savedReportsHeading = await screen.findByText("Saved reports");
    expect(savedReportsHeading).toBeInTheDocument();

    const reportName = await screen.findByText("Q1 Report");
    expect(reportName).toBeInTheDocument();
  });

  it("deletes a saved report", async () => {
    let callCount = 0;
    const fetchMock = createFetchMock({
      "/v1/saved-reports": (url: string, method: string) => {
        if (url.includes("/report-1") && method === "DELETE") {
          return response(null, 204);
        }
        if (url.includes("/report-1")) {
          return response(mockSavedReport);
        }
        // For list calls
        callCount++;
        if (callCount > 1) return response({ reports: [] });
        return response({ reports: [mockSavedReport] });
      },
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<Analytics apiKey="key" baseURL="/api" />);

    await screen.findByText("Saved reports");

    const deleteButton = screen.getByRole("button", { name: "Delete" });
    fireEvent.click(deleteButton);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/v1/saved-reports/report-1"),
        expect.objectContaining({ method: "DELETE" })
      );
    });
  });

  it("switches between campaign and journey report types", async () => {
    render(<Analytics apiKey="key" baseURL="/api" />);

    await screen.findByText("Funnel over time");

    const reportTypeSelect = screen.getByLabelText("Report type");
    fireEvent.change(reportTypeSelect, { target: { value: "journey" } });

    await waitFor(() => {
      // Verify the component handles the switch without crashing
      expect(screen.getByLabelText("Report type")).toHaveValue("journey");
    });
  });

  it("shows loading spinner while fetching initial data", () => {
    const slowFetch = vi.fn(() => new Promise(() => {})); // Never resolves
    vi.stubGlobal("fetch", slowFetch);

    render(<Analytics apiKey="key" baseURL="/api" />);

    const spinners = screen.getAllByRole("status");
    expect(spinners.length).toBeGreaterThan(0);
  });

  it("shows empty state when no campaigns or journeys exist", async () => {
    const emptyFetch = createFetchMock({
      "/v1/campaigns": () => response([]),
      "/v1/journeys": () => response({ journeys: [] }),
    });
    vi.stubGlobal("fetch", emptyFetch);

    render(<Analytics apiKey="key" baseURL="/api" />);

    const emptyState = await screen.findByText("No campaigns available");
    expect(emptyState).toBeInTheDocument();
  });

  it("displays error message when report fetching fails", async () => {
    const errorFetch = createFetchMock({
      "/reports/campaigns/campaign-1/funnel-over-time": () => Promise.reject(new Error("Network error")),
    });
    vi.stubGlobal("fetch", errorFetch);

    render(<Analytics apiKey="key" baseURL="/api" />);

    const errorMessage = await screen.findByText(/Network error/);
    expect(errorMessage).toBeInTheDocument();
  });
});
