import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { AppShell } from "./AppShell";

type View = "overview" | "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "analytics" | "copilots" | "assistant" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging" | "flags" | "catalogs" | "prompts";

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
  assistant: ["AI Assistant", "Conversational analytics assistant grounded in report data and audited tools."],
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

beforeEach(() => {
  const modalRoot = document.createElement("div");
  modalRoot.id = "modal-root";
  document.body.appendChild(modalRoot);

  // Default to desktop viewport (not mobile)
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
  );
});

afterEach(() => {
  const modalRoot = document.getElementById("modal-root");
  if (modalRoot) {
    modalRoot.remove();
  }
  vi.unstubAllGlobals();
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

  describe("mobile navigation", () => {
    beforeEach(() => {
      vi.stubGlobal("matchMedia", vi.fn().mockImplementation((query: string) => ({
        matches: query === "(max-width: 760px)",
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })));
    });

    it("shows menu button on mobile viewport", async () => {
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

      await waitFor(() => {
        const menuButton = screen.getByTestId("mobile-menu-button");
        expect(menuButton).toBeInTheDocument();
        expect(menuButton).toHaveAttribute("aria-label", "Open navigation menu");
      });
    });

    it("toggles mobile nav drawer with menu button", async () => {
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

      await waitFor(() => {
        const menuButton = screen.getByTestId("mobile-menu-button");
        expect(menuButton).toBeInTheDocument();
      });

      const menuButton = screen.getByTestId("mobile-menu-button");
      fireEvent.click(menuButton);

      await waitFor(() => {
        expect(screen.getByTestId("mobile-nav-drawer")).toBeInTheDocument();
        expect(menuButton).toHaveAttribute("aria-expanded", "true");
      });

      fireEvent.click(menuButton);

      await waitFor(() => {
        expect(screen.queryByTestId("mobile-nav-drawer")).not.toBeInTheDocument();
        expect(menuButton).toHaveAttribute("aria-expanded", "false");
      });
    });

    it("closes mobile nav drawer with Esc key (focus trap)", async () => {
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

      await waitFor(() => {
        const menuButton = screen.getByTestId("mobile-menu-button");
        expect(menuButton).toBeInTheDocument();
      });

      const menuButton = screen.getByTestId("mobile-menu-button");
      fireEvent.click(menuButton);

      await waitFor(() => {
        expect(screen.getByTestId("mobile-nav-drawer")).toBeInTheDocument();
      });

      fireEvent.keyDown(document, { key: "Escape" });

      await waitFor(() => {
        expect(screen.queryByTestId("mobile-nav-drawer")).not.toBeInTheDocument();
      });
    });

    it("closes mobile nav drawer when a view is selected", async () => {
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

      await waitFor(() => {
        const menuButton = screen.getByTestId("mobile-menu-button");
        expect(menuButton).toBeInTheDocument();
      });

      const menuButton = screen.getByTestId("mobile-menu-button");
      fireEvent.click(menuButton);

      await waitFor(() => {
        expect(screen.getByTestId("mobile-nav-drawer")).toBeInTheDocument();
      });

      const schemasButton = screen.getByRole("button", { name: /Event schemas/i });
      fireEvent.click(schemasButton);

      await waitFor(() => {
        expect(screen.queryByTestId("mobile-nav-drawer")).not.toBeInTheDocument();
        expect(onViewChange).toHaveBeenCalledWith("schemas");
      });
    });

    it("traps focus within mobile nav drawer", async () => {
      const onViewChange = vi.fn();
      const { container } = render(
        <AppShell
          view="profiles"
          onViewChange={onViewChange}
          viewTitles={viewTitles}
          healthy={true}
        >
          <div>Test content</div>
        </AppShell>
      );

      await waitFor(() => {
        const menuButton = screen.getByTestId("mobile-menu-button");
        expect(menuButton).toBeInTheDocument();
      });

      const menuButton = screen.getByTestId("mobile-menu-button");
      fireEvent.click(menuButton);

      await waitFor(() => {
        const drawer = screen.getByTestId("mobile-nav-drawer");
        expect(drawer).toBeInTheDocument();
        expect(screen.getByTestId("mobile-menu-button")).toHaveAttribute("aria-controls", "mobile-nav-drawer");
      });

      const drawer = screen.getByTestId("mobile-nav-drawer");
      const focusableElements = Array.from(
        drawer.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')
      ) as HTMLElement[];

      if (focusableElements.length > 0) {
        const lastElement = focusableElements[focusableElements.length - 1];
        lastElement.focus();
        expect(document.activeElement).toBe(lastElement);

        fireEvent.keyDown(document, { key: "Tab", shiftKey: false });

        await waitFor(() => {
          expect(document.activeElement).toBe(focusableElements[0]);
        });
      }
    });

    it("restores focus to menu button when drawer closes", async () => {
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

      await waitFor(() => {
        const menuButton = screen.getByTestId("mobile-menu-button");
        expect(menuButton).toBeInTheDocument();
      });

      const menuButton = screen.getByTestId("mobile-menu-button");
      menuButton.focus();
      expect(menuButton).toHaveFocus();

      fireEvent.click(menuButton);

      await waitFor(() => {
        expect(screen.getByTestId("mobile-nav-drawer")).toBeInTheDocument();
      });

      fireEvent.keyDown(document, { key: "Escape" });

      await waitFor(() => {
        expect(screen.queryByTestId("mobile-nav-drawer")).not.toBeInTheDocument();
        expect(menuButton).toHaveFocus();
      });
    });
  });

  describe("accessibility landmarks", () => {
    it("has a skip-to-content link that is focusable", () => {
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

      const skipLink = screen.getByText("Skip to main content");
      expect(skipLink).toBeInTheDocument();
      expect(skipLink).toHaveAttribute("href", "#main-content");
    });

    it("skip link moves focus to main content", () => {
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

      const skipLink = screen.getByText("Skip to main content") as HTMLAnchorElement;
      const mainElement = screen.getByRole("main");

      skipLink.click();

      expect(mainElement).toHaveFocus();
    });

    it("has a main landmark with id main-content", () => {
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

      const mainElement = screen.getByRole("main");
      expect(mainElement).toHaveAttribute("id", "main-content");
    });

    it("has a navigation landmark with aria-label", () => {
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

      const navElements = screen.getAllByRole("navigation");
      expect(navElements.length).toBeGreaterThan(0);
      navElements.forEach((nav) => {
        expect(nav).toHaveAttribute("aria-label");
      });
    });

    it("has a page header with h1", () => {
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

      const h1 = screen.getByRole("heading", { level: 1 });
      expect(h1).toHaveTextContent("Profiles");
    });

    it("all icon-only buttons have accessible names", () => {
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

      // Get all buttons
      const buttons = screen.getAllByRole("button");

      buttons.forEach((button) => {
        // Check if button has text content, aria-label, or aria-labelledby
        const hasText = button.textContent?.trim().length || 0 > 0;
        const hasAriaLabel = button.getAttribute("aria-label");
        const hasAriaLabelledBy = button.getAttribute("aria-labelledby");

        const hasAccessibleName = hasText || hasAriaLabel || hasAriaLabelledBy;
        expect(hasAccessibleName).toBeTruthy();
      });
    });
  });
});
