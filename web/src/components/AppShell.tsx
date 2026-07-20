import React, { ReactNode, useEffect, useState } from "react";
import { Sidebar } from "./Sidebar";
import { PageHeader } from "./PageHeader";
import { CommandPalette } from "./CommandPalette";

type View = "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging";

interface AppShellProps {
  view: View;
  onViewChange: (view: View) => void;
  viewTitles: Record<View, [string, string]>;
  healthy: boolean | null;
  children: ReactNode;
  theme?: string;
  onThemeToggle?: () => void;
  onSignOut?: () => void;
}

export const AppShell = React.forwardRef<HTMLDivElement, AppShellProps>(
  ({ view, onViewChange, viewTitles, healthy, children, theme, onThemeToggle, onSignOut }, ref) => {
    const [paletteOpen, setPaletteOpen] = useState(false);

    useEffect(() => {
      const handleKeydown = (event: KeyboardEvent) => {
        if ((event.metaKey || event.ctrlKey) && event.key === "k") {
          event.preventDefault();
          setPaletteOpen(true);
        }
      };

      document.addEventListener("keydown", handleKeydown);
      return () => {
        document.removeEventListener("keydown", handleKeydown);
      };
    }, []);

    return (
      <div className="shell" ref={ref}>
        <Sidebar
          view={view}
          onViewChange={onViewChange}
          viewTitles={viewTitles}
          healthy={healthy}
          theme={theme}
          onThemeToggle={onThemeToggle}
          onSignOut={onSignOut}
        />
        <main>
          <PageHeader
            title={viewTitles[view][0]}
            description={viewTitles[view][1]}
          />
          {children}
        </main>
        <CommandPalette
          isOpen={paletteOpen}
          onClose={() => setPaletteOpen(false)}
          onNavigate={onViewChange}
          currentView={view}
        />
      </div>
    );
  }
);

AppShell.displayName = "AppShell";
