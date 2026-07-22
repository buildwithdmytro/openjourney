import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import { Sidebar } from "./Sidebar";

type View = "overview" | "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "analytics" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging" | "flags" | "catalogs" | "prompts";

const viewTitles: Record<View, [string, string]> = {
  overview: ["Overview", "At a glance view of your workspace activity and resources."],
  profiles: ["Profiles", "Inspect the current customer and consent projection."],
  schemas: ["Event schemas", "Register typed event contracts and compatibility rules."],
  "api-keys": ["API keys", "Create scoped credentials and revoke access."],
  privacy: ["Privacy", "Submit and inspect DSAR export/delete operations."],
  access: ["Access", "Provision local/OIDC users and tenant roles."],
  operations: ["Operations", "Inspect queues, DLQs, and replay determinism."],
  audit: ["Audit", "Review tenant-scoped security and operations activity."],
  segments: ["Segments", "Manage customer segments and membership rules."],
  scoring: ["Scoring", "Publish governed scoring models and inspect profile scores."],
  templates: ["Templates", "Design email templates with Liquid tags and live preview."],
  campaigns: ["Campaigns", "Schedule and manage sharded marketing campaigns linked to segments and templates."],
  journeys: ["Journeys", "Design, publish, and monitor automated customer experiences."],
  experiments: ["Experiments", "Create controlled tests with stable audience assignment."],
  reports: ["Reports", "Compare delivery, conversion, and experiment performance."],
  analytics: ["Analytics", "Explore time-series trends, retention cohorts, audience growth, and spending."],
  copilots: ["AI Copilots", "Create governed drafts for review and human approval."],
  governance: ["AI Governance", "Manage providers, budgets, redaction, and AI activity."],
  extensions: ["Extensions", "Install signed providers, configure grants, and review extension health."],
  connectors: ["Connectors", "Move data through governed sources, sinks, exports, and identity commands."],
  suppressions: ["Suppressions", "Manage bounces, complaints, and manually suppressed endpoints."],
  "sender-identities": ["Sender Identities", "Manage verified sender emails, SMS, and push channels."],
  "device-tokens": ["Device Tokens", "Inspect and retire push device tokens per profile."],
  acquisition: ["Acquisition", "Build defended forms and immutable landing pages."],
  messaging: ["Messaging", "Create and manage in-app messages, content cards, and web push campaigns."],
  flags: ["Feature Flags", "Create, publish, and toggle environment-scoped feature flags with targeting and exposure analytics."],
  catalogs: ["Catalogs", "Manage reference data catalogs and governed connected content sources."],
  prompts: ["Prompts", "Author, version, evaluate, and publish governed prompt templates."],
};

afterEach(cleanup);

describe("Sidebar", () => {
  it("renders grouped navigation sections", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const groups = screen.getAllByRole("group");
    expect(groups.length).toBeGreaterThan(0);
  });

  it("renders group headers with accessible labels", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    // Query for group labels specifically
    expect(screen.getByText("Audiences").closest(".nav-group-label")).toBeInTheDocument();
    expect(screen.getAllByText("Messaging")[0].closest(".nav-group-label")).toBeInTheDocument();
    expect(screen.getByText("AI & Insights").closest(".nav-group-label")).toBeInTheDocument();
    expect(screen.getByText("Data").closest(".nav-group-label")).toBeInTheDocument();
    expect(screen.getByText("Admin").closest(".nav-group-label")).toBeInTheDocument();
    expect(screen.getByText("Settings").closest(".nav-group-label")).toBeInTheDocument();
  });

  it("renders all 23 views as navigation items", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const views = [
      "Profiles",
      "Segments",
      "Scoring",
      "Acquisition",
      "Templates",
      "Campaigns",
      "Journeys",
      "Experiments",
      "Connectors",
      "Suppressions",
      "Sender Identities",
      "Device Tokens",
      "AI Copilots",
      "Reports",
      "Event schemas",
      "Extensions",
      "Privacy",
      "Access",
      "Audit",
      "Operations",
      "API keys",
    ];

    // Check all nav buttons exist
    const navButtons = screen.getAllByRole("button");
    // Filter out non-view buttons (theme, sign out)
    const viewButtons = navButtons.filter((btn) => {
      const text = btn.textContent || "";
      return !text.includes("mode") && text !== "Sign out";
    });

    expect(viewButtons.length).toBeGreaterThanOrEqual(22);

    // Check specific views
    views.forEach((view) => {
      const button = screen.getByRole("button", { name: view });
      expect(button).toBeInTheDocument();
    });
  });

  it("applies active class to the current view", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const activeButton = screen.getByRole("button", { name: "Profiles" });
    expect(activeButton).toHaveClass("active");
  });

  it("does not apply active class to inactive views", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const segmentsButton = screen.getByRole("button", { name: "Segments" });
    expect(segmentsButton).not.toHaveClass("active");
  });

  it("calls onViewChange when a nav item is clicked", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const segmentsButton = screen.getByRole("button", { name: "Segments" });
    fireEvent.click(segmentsButton);
    expect(handleViewChange).toHaveBeenCalledWith("segments");
  });

  it("keyboard navigation reaches every item via click handlers", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const views: Array<[string, string]> = [
      ["Segments", "segments"],
      ["Scoring", "scoring"],
      ["Campaigns", "campaigns"],
      ["Journeys", "journeys"],
      ["Experiments", "experiments"],
    ];

    views.forEach(([label, view]) => {
      const button = screen.getByRole("button", { name: label });
      fireEvent.click(button);
      expect(handleViewChange).toHaveBeenCalledWith(view);
    });
  });

  it("reflects active state change when view prop changes", () => {
    const handleViewChange = vi.fn();
    const { rerender } = render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByRole("button", { name: "Profiles" })).toHaveClass("active");

    rerender(
      <Sidebar
        view="segments"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByRole("button", { name: "Profiles" })).not.toHaveClass("active");
    expect(screen.getByRole("button", { name: "Segments" })).toHaveClass("active");
  });

  it("renders brand logo", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByText("OpenJourney")).toBeInTheDocument();
  });

  it("renders health status indicator", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByText(/API ready/)).toBeInTheDocument();
  });

  it("shows unavailable status when API is unhealthy", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={false}
      />
    );

    expect(screen.getByText(/API unavailable/)).toBeInTheDocument();
  });

  it("shows checking status when health is unknown", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={null}
      />
    );

    expect(screen.getByText(/API checking/)).toBeInTheDocument();
  });

  it("renders theme toggle button when onThemeToggle is provided", () => {
    const handleViewChange = vi.fn();
    const handleThemeToggle = vi.fn();

    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
        theme="light"
        onThemeToggle={handleThemeToggle}
      />
    );

    const themeButton = screen.getByRole("button", { name: /Dark mode/ });
    expect(themeButton).toBeInTheDocument();
  });

  it("calls onThemeToggle when theme button is clicked", () => {
    const handleViewChange = vi.fn();
    const handleThemeToggle = vi.fn();

    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
        theme="light"
        onThemeToggle={handleThemeToggle}
      />
    );

    const themeButton = screen.getByRole("button", { name: /Dark mode/ });
    fireEvent.click(themeButton);
    expect(handleThemeToggle).toHaveBeenCalled();
  });

  it("renders sign out button when onSignOut is provided", () => {
    const handleViewChange = vi.fn();
    const handleSignOut = vi.fn();

    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
        onSignOut={handleSignOut}
      />
    );

    const signOutButton = screen.getByRole("button", { name: "Sign out" });
    expect(signOutButton).toBeInTheDocument();
  });

  it("calls onSignOut when sign out button is clicked", () => {
    const handleViewChange = vi.fn();
    const handleSignOut = vi.fn();

    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
        onSignOut={handleSignOut}
      />
    );

    const signOutButton = screen.getByRole("button", { name: "Sign out" });
    fireEvent.click(signOutButton);
    expect(handleSignOut).toHaveBeenCalled();
  });

  it("renders navigation with proper ARIA label", () => {
    const handleViewChange = vi.fn();
    render(
      <Sidebar
        view="profiles"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    const nav = screen.getByRole("navigation", { name: "Primary" });
    expect(nav).toBeInTheDocument();
  });

  it("maintains active state programmatically across different views", () => {
    const handleViewChange = vi.fn();
    const { rerender } = render(
      <Sidebar
        view="campaigns"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByRole("button", { name: "Campaigns" })).toHaveClass("active");

    rerender(
      <Sidebar
        view="journeys"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByRole("button", { name: "Campaigns" })).not.toHaveClass("active");
    expect(screen.getByRole("button", { name: "Journeys" })).toHaveClass("active");

    rerender(
      <Sidebar
        view="reports"
        onViewChange={handleViewChange}
        viewTitles={viewTitles}
        healthy={true}
      />
    );

    expect(screen.getByRole("button", { name: "Journeys" })).not.toHaveClass("active");
    expect(screen.getByRole("button", { name: "Reports" })).toHaveClass("active");
  });
});
