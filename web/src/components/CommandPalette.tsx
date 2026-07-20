import React, { useEffect, useRef, useState } from "react";
import Modal from "./Modal";

type View = "overview" | "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging";

interface PaletteItem {
  label: string;
  view?: View;
  action?: () => void;
  category: string;
}

const navGroups = [
  {
    label: "",
    items: ["overview"],
  },
  {
    label: "Audiences",
    items: ["profiles", "segments", "scoring", "acquisition"],
  },
  {
    label: "Messaging",
    items: ["templates", "campaigns", "journeys", "experiments", "messaging", "connectors", "suppressions", "sender-identities", "device-tokens"],
  },
  {
    label: "AI & Insights",
    items: ["copilots", "governance", "reports"],
  },
  {
    label: "Data",
    items: ["schemas"],
  },
  {
    label: "Admin",
    items: ["extensions", "privacy", "access", "audit", "operations"],
  },
  {
    label: "Settings",
    items: ["api-keys"],
  },
];

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

export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onNavigate: (view: View) => void;
  currentView: View;
}

export const CommandPalette = React.forwardRef<HTMLDivElement, CommandPaletteProps>(
  ({ isOpen, onClose, onNavigate, currentView }, ref) => {
    const [query, setQuery] = useState("");
    const [selectedIndex, setSelectedIndex] = useState(0);
    const inputRef = useRef<HTMLInputElement>(null);

    const items: PaletteItem[] = [];
    navGroups.forEach((group) => {
      group.items.forEach((view) => {
        items.push({
          label: viewTitles[view as View][0],
          view: view as View,
          category: group.label,
        });
      });
    });

    const filteredItems = query
      ? items.filter((item) =>
          item.label.toLowerCase().includes(query.toLowerCase())
        )
      : items;

    useEffect(() => {
      setQuery("");
      setSelectedIndex(0);
      if (isOpen && inputRef.current) {
        inputRef.current.focus();
      }
    }, [isOpen]);

    const handleKeydown = (event: React.KeyboardEvent) => {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((prev) => (prev + 1) % filteredItems.length);
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((prev) =>
          prev === 0 ? filteredItems.length - 1 : prev - 1
        );
      } else if (event.key === "Enter") {
        event.preventDefault();
        const item = filteredItems[selectedIndex];
        if (item && item.view) {
          onNavigate(item.view);
          onClose();
        }
      }
    };

    return (
      <Modal
        isOpen={isOpen}
        onClose={onClose}
        aria-label="Command palette"
        ref={ref}
      >
        <div className="command-palette">
          <input
            ref={inputRef}
            type="text"
            placeholder="Search views... (⌘K)"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            onKeyDown={handleKeydown}
            className="command-palette-input"
          />
          <div className="command-palette-list" role="listbox">
            {filteredItems.length === 0 ? (
              <div className="command-palette-empty">No views found</div>
            ) : (
              filteredItems.map((item, index) => (
                <div
                  key={`${item.category}-${item.label}`}
                  className={`command-palette-item ${
                    index === selectedIndex ? "selected" : ""
                  }`}
                  role="option"
                  aria-selected={index === selectedIndex}
                  onClick={() => {
                    if (item.view) {
                      onNavigate(item.view);
                      onClose();
                    }
                  }}
                >
                  <div className="command-palette-item-content">
                    <div className="command-palette-item-label">{item.label}</div>
                    <div className="command-palette-item-category">
                      {item.category}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </Modal>
    );
  }
);

CommandPalette.displayName = "CommandPalette";
