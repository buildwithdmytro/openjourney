import React from "react";
import Icon from "./Icon";

interface PageHeaderProps {
  title: string;
  description: string;
  onMenuClick?: () => void;
  menuOpen?: boolean;
}

export const PageHeader = React.forwardRef<HTMLElement, PageHeaderProps>(
  ({ title, description, onMenuClick, menuOpen }, ref) => {
    return (
      <header ref={ref} className="page-header">
        <div className="page-header-content">
          {onMenuClick && (
            <button
              className="menu-button"
              onClick={onMenuClick}
              aria-label={menuOpen ? "Close navigation menu" : "Open navigation menu"}
              aria-expanded={menuOpen}
              aria-controls="mobile-nav-drawer"
              data-testid="mobile-menu-button"
            >
              <Icon name="menu" size={20} />
            </button>
          )}
          <div className="page-header-text">
            <p>Platform kernel</p>
            <h1>{title}</h1>
            <span>{description}</span>
          </div>
        </div>
      </header>
    );
  }
);

PageHeader.displayName = "PageHeader";
