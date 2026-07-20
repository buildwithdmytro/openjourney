import React from "react";
import Icon from "./Icon";

export interface ErrorStateProps extends React.HTMLAttributes<HTMLDivElement> {
  icon?: "warn" | "close" | "info";
  title?: string;
  description: string;
  onRetry?: () => void;
}

const ErrorState = React.forwardRef<HTMLDivElement, ErrorStateProps>(
  ({ icon = "warn", title, description, onRetry, className, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={`error-state ${className || ""}`.trim()}
        {...props}
      >
        {icon && (
          <div className="error-state-icon">
            <Icon name={icon} size={48} aria-hidden />
          </div>
        )}
        <h3 className="error-state-title">{title || "Error"}</h3>
        <p className="error-state-description">{description}</p>
        {onRetry && (
          <button className="error-state-cta" onClick={onRetry}>
            Retry
          </button>
        )}
      </div>
    );
  }
);

ErrorState.displayName = "ErrorState";

export default ErrorState;
