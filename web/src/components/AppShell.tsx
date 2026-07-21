import React, { ReactNode, useEffect, useRef, useState } from "react";
import { Sidebar } from "./Sidebar";
import { PageHeader } from "./PageHeader";
import { CommandPalette } from "./CommandPalette";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { useFocusTrap } from "../hooks/useFocusTrap";

type View = "overview" | "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging" | "flags";

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
    const [mobileNavOpen, setMobileNavOpen] = useState(false);
    const isMobile = useMediaQuery("(max-width: 760px)");
    const mobileNavRef = useRef<HTMLDivElement>(null);

    useFocusTrap(mobileNavOpen && isMobile, mobileNavRef, () => setMobileNavOpen(false));

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

    const handleViewChange = (newView: View) => {
      onViewChange(newView);
      if (isMobile) {
        setMobileNavOpen(false);
      }
    };

    const mainRef = useRef<HTMLElement>(null);

    const handleSkipToContent = () => {
      mainRef.current?.focus();
    };

    return (
      <div className={`shell ${isMobile ? "mobile" : ""}`} ref={ref}>
        <a href="#main-content" className="skip-to-content" onClick={handleSkipToContent}>
          Skip to main content
        </a>
        {!isMobile && (
          <Sidebar
            view={view}
            onViewChange={handleViewChange}
            viewTitles={viewTitles}
            healthy={healthy}
            theme={theme}
            onThemeToggle={onThemeToggle}
            onSignOut={onSignOut}
          />
        )}
        {isMobile && mobileNavOpen && (
          <>
            <div
              className="mobile-nav-backdrop"
              onClick={() => setMobileNavOpen(false)}
              aria-hidden="true"
            />
            <nav
              ref={mobileNavRef}
              className="mobile-nav-drawer"
              aria-label="Primary"
              data-testid="mobile-nav-drawer"
            >
              <Sidebar
                view={view}
                onViewChange={handleViewChange}
                viewTitles={viewTitles}
                healthy={healthy}
                theme={theme}
                onThemeToggle={onThemeToggle}
                onSignOut={onSignOut}
              />
            </nav>
          </>
        )}
        <main ref={mainRef} id="main-content" tabIndex={-1}>
          <PageHeader
            title={viewTitles[view][0]}
            description={viewTitles[view][1]}
            onMenuClick={isMobile ? () => setMobileNavOpen(!mobileNavOpen) : undefined}
            menuOpen={isMobile ? mobileNavOpen : undefined}
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
