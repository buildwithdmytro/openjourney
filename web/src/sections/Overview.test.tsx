import { render, screen, waitFor } from "@testing-library/react";
import { vi, describe, it, expect, beforeEach, afterEach } from "vitest";
import Overview from "./Overview";

vi.mock("../api", () => ({
  getOverview: vi.fn(),
}));

import * as api from "../api";

const mockApiCall = api.getOverview as ReturnType<typeof vi.fn>;

describe("Overview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders loading state", () => {
    mockApiCall.mockImplementation(
      () =>
        new Promise(() => {
          // never resolve
        })
    );
    const { container } = render(<Overview apiKey="test-key" baseURL="http://api" />);
    expect(container.querySelector('[role="status"]')).toBeInTheDocument();
  });

  it("displays overview data when loaded", async () => {
    mockApiCall.mockResolvedValue({
      profiles: 150,
      journeys: 5,
      campaigns: 12,
      delivery_attempts: 1250,
      inapp_messages: 45,
      connector_runs: 89,
    });

    render(<Overview apiKey="test-key" baseURL="http://api" />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    });

    expect(screen.getByText("150")).toBeInTheDocument();
    expect(screen.getByText("5")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.getByText("1,250")).toBeInTheDocument();
    expect(screen.getByText("45")).toBeInTheDocument();
    expect(screen.getByText("89")).toBeInTheDocument();
  });

  it("shows labels for each card", async () => {
    mockApiCall.mockResolvedValue({
      profiles: 100,
      journeys: 5,
      campaigns: 10,
      delivery_attempts: 500,
      inapp_messages: 20,
      connector_runs: 50,
    });

    const { container } = render(<Overview apiKey="test-key" baseURL="http://api" />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    });

    const allH3 = container.querySelectorAll("h3");
    expect(allH3.length).toBeGreaterThanOrEqual(6);
    const labels = Array.from(allH3).map((el) => el.textContent);
    expect(labels.some((l) => l === "Profiles")).toBe(true);
    expect(labels.some((l) => l === "Journeys")).toBe(true);
    expect(labels.some((l) => l === "Campaigns")).toBe(true);
    expect(labels.some((l) => l === "Delivery Attempts")).toBe(true);
    expect(labels.some((l) => l === "In-App Messages")).toBe(true);
    expect(labels.some((l) => l === "Connector Runs")).toBe(true);
  });

  it("includes navigation links to sections", async () => {
    mockApiCall.mockResolvedValue({
      profiles: 100,
      journeys: 5,
      campaigns: 10,
      delivery_attempts: 500,
      inapp_messages: 20,
      connector_runs: 50,
    });

    const { container } = render(<Overview apiKey="test-key" baseURL="http://api" />);

    await waitFor(() => {
      expect(container.querySelector(".overview-grid")).toBeInTheDocument();
    });

    const links = screen.getAllByRole("link");
    expect(links.length).toBeGreaterThan(0);
    expect(links.some((l) => l.getAttribute("href") === "#profiles")).toBe(true);
    expect(links.some((l) => l.getAttribute("href") === "#journeys")).toBe(true);
    expect(links.some((l) => l.getAttribute("href") === "#campaigns")).toBe(true);
  });

  it("shows empty state for empty workspace", async () => {
    mockApiCall.mockResolvedValue({
      profiles: 0,
      journeys: 0,
      campaigns: 0,
      delivery_attempts: 0,
      inapp_messages: 0,
      connector_runs: 0,
    });

    render(<Overview apiKey="test-key" baseURL="http://api" />);

    await waitFor(() => {
      expect(screen.getByText("Welcome to OpenJourney")).toBeInTheDocument();
    });

    expect(screen.getByText(/Your workspace is ready/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Explore Profiles" })).toBeInTheDocument();
  });

  it("handles fetch error", async () => {
    mockApiCall.mockRejectedValue(new Error("Network error"));

    render(<Overview apiKey="test-key" baseURL="http://api" />);

    await waitFor(() => {
      expect(screen.getByText("Could not load overview")).toBeInTheDocument();
    });

    expect(screen.getByText("Network error")).toBeInTheDocument();
  });

  it("shows page description", async () => {
    mockApiCall.mockResolvedValue({
      profiles: 100,
      journeys: 5,
      campaigns: 10,
      delivery_attempts: 500,
      inapp_messages: 20,
      connector_runs: 50,
    });

    const { container } = render(<Overview apiKey="test-key" baseURL="http://api" />);

    await waitFor(() => {
      expect(container.querySelector(".overview-grid")).toBeInTheDocument();
    });

    const descriptions = screen.getAllByText("At a glance view of your workspace activity and resources.");
    expect(descriptions.length).toBeGreaterThan(0);
  });
});
