import React from "react";
import Icon from "./Icon";

export interface EmptyStateCTA {
  label: string;
  onClick: () => void;
}

export interface EmptyStateProps extends React.HTMLAttributes<HTMLDivElement> {
  icon?: "search" | "close" | "check" | "chevron" | "plus" | "trash" | "warn" | "info" | "menu" | "external" | "sun" | "moon";
  title: string;
  description?: string;
  cta?: EmptyStateCTA;
}

const EmptyState = React.forwardRef<HTMLDivElement, EmptyStateProps>(
  ({ icon, title, description, cta, className, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={`empty-state ${className || ""}`.trim()}
        {...props}
      >
        {icon && (
          <div className="empty-state-icon">
            <Icon name={icon} size={48} aria-hidden />
          </div>
        )}
        <h3 className="empty-state-title">{title}</h3>
        {description && (
          <p className="empty-state-description">{description}</p>
        )}
        {cta && (
          <button className="empty-state-cta" onClick={cta.onClick}>
            {cta.label}
          </button>
        )}
      </div>
    );
  }
);

EmptyState.displayName = "EmptyState";

export default EmptyState;
