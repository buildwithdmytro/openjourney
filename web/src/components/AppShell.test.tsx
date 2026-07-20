import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { AppShell } from "./AppShell";

type View = "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging";

const viewTitles: Record<View, [string, string]> = {
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
  copilots: ["AI Copilots", "Create governed drafts for review and human approval."],
  governance: ["AI Governance", "Manage providers, budgets, redaction, and AI activity."],
  extensions: ["Extensions", "Install signed providers, configure grants, and review extension health."],
  connectors: ["Connectors", "Move data through governed sources, sinks, exports, and identity commands."],
  suppressions: ["Suppressions", "Manage bounces, complaints, and manually suppressed endpoints."],
  "sender-identities": ["Sender Identities", "Manage verified sender emails, SMS, and push channels."],
  "device-tokens": ["Device Tokens", "Inspect and retire push device tokens per profile."],
  acquisition: ["Acquisition", "Build defended forms and immutable landing pages."],
  messaging: ["Messaging", "Create and manage in-app messages, content cards, and web push campaigns."],
};

afterEach(cleanup);

beforeEach(() => {
  const modalRoot = document.createElement("div");
  modalRoot.id = "modal-root";
  document.body.appendChild(modalRoot);
});

afterEach(() => {
  const modalRoot = document.getElementById("modal-root");
  if (modalRoot) {
    modalRoot.remove();
  }
});

describe("AppShell", () => {
  it("renders with sidebar and main content", () => {
    const onViewChange = vi.fn();
    render(
      <AppShell
        view="profiles"
        onViewChange={onViewChange}
        viewTitles={viewTitles}
        healthy={true}
      >
        <div>Test content</div>
      </AppShell>
    );

    expect(screen.getByText("Test content")).toBeInTheDocument();
    expect(screen.getByRole("navigation")).toBeInTheDocument();
  });

  it("opens command palette with Cmd+K", async () => {
    const onViewChange = vi.fn();
    render(
      <AppShell
        view="profiles"
        onViewChange={onViewChange}
        viewTitles={viewTitles}
        healthy={true}
      >
        <div>Test content</div>
      </AppShell>
    );

    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search views/i)).toBeInTheDocument();
    });
  });

  it("opens command palette with Ctrl+K", async () => {
    const onViewChange = vi.fn();
    render(
      <AppShell
        view="profiles"
        onViewChange={onViewChange}
        viewTitles={viewTitles}
        healthy={true}
      >
        <div>Test content</div>
      </AppShell>
    );

    fireEvent.keyDown(document, { key: "k", ctrlKey: true });

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search views/i)).toBeInTheDocument();
    });
  });

  it("navigates to view from command palette", async () => {
    const onViewChange = vi.fn();
    render(
      <AppShell
        view="profiles"
        onViewChange={onViewChange}
        viewTitles={viewTitles}
        healthy={true}
      >
        <div>Test content</div>
      </AppShell>
    );

    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => {
      const input = screen.getByPlaceholderText(/search views/i);
      expect(input).toBeInTheDocument();
    });

    const input = screen.getByPlaceholderText(/search views/i);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(onViewChange).toHaveBeenCalled();
    });
  });

  it("closes command palette with Esc", async () => {
    const onViewChange = vi.fn();
    render(
      <AppShell
        view="profiles"
        onViewChange={onViewChange}
        viewTitles={viewTitles}
        healthy={true}
      >
        <div>Test content</div>
      </AppShell>
    );

    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search views/i)).toBeInTheDocument();
    });

    const input = screen.getByPlaceholderText(/search views/i);
    fireEvent.keyDown(input, { key: "Escape" });

    await waitFor(() => {
      expect(screen.queryByPlaceholderText(/search views/i)).not.toBeInTheDocument();
    });
  });
});
