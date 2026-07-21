import React from "react";

type View = "overview" | "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "analytics" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging" | "flags";

interface NavGroup {
  label: string;
  items: View[];
}

const navGroups: NavGroup[] = [
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
    items: ["templates", "campaigns", "journeys", "experiments", "flags", "messaging", "connectors", "suppressions", "sender-identities", "device-tokens"],
  },
  {
    label: "AI & Insights",
    items: ["copilots", "governance", "reports", "analytics"],
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

interface SidebarProps {
  view: View;
  onViewChange: (view: View) => void;
  viewTitles: Record<View, [string, string]>;
  healthy: boolean | null;
  theme?: string;
  onThemeToggle?: () => void;
  onSignOut?: () => void;
}

export const Sidebar = React.forwardRef<HTMLElement, SidebarProps>(
  ({ view, onViewChange, viewTitles, healthy, theme, onThemeToggle, onSignOut }, ref) => {
    return (
      <aside ref={ref}>
        <div className="brand"><span>O</span> OpenJourney</div>
        <nav aria-label="Primary">
          {navGroups.map((group) => (
            <div key={group.label} role="group">
              <div className="nav-group-label">{group.label}</div>
              {group.items.map((item) => (
                <button
                  key={item}
                  className={view === item ? "active" : ""}
                  onClick={() => onViewChange(item)}
                >
                  {viewTitles[item][0]}
                </button>
              ))}
            </div>
          ))}
        </nav>
        <div style={{ marginTop: "auto", padding: "16px 0 0 0", borderTop: "1px solid rgba(255,255,255,0.1)", display: "flex", flexDirection: "column", gap: "8px" }}>
          {onThemeToggle && (
            <button className="secondary small" onClick={onThemeToggle} style={{ width: "100%", background: "transparent", border: "1px solid rgba(255,255,255,0.2)", color: "var(--color-ink-muted)", padding: "8px", borderRadius: "6px", cursor: "pointer", fontSize: "12px", fontWeight: "bold" }}>
              {theme === "light" ? "Dark" : "Light"} mode
            </button>
          )}
          {onSignOut && (
            <button className="secondary small" onClick={onSignOut} style={{ width: "100%", background: "transparent", border: "1px solid rgba(255,255,255,0.2)", color: "var(--color-ink-muted)", padding: "8px", borderRadius: "6px", cursor: "pointer", fontSize: "12px", fontWeight: "bold" }}>
              Sign out
            </button>
          )}
          <div className={`health ${healthy ? "up" : ""}`}>
            <i /> API {healthy === null ? "checking" : healthy ? "ready" : "unavailable"}
          </div>
        </div>
      </aside>
    );
  }
);

Sidebar.displayName = "Sidebar";
