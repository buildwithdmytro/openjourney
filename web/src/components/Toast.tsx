import React, { useEffect } from "react";
import Icon from "./Icon";

export interface ToastProps extends React.HTMLAttributes<HTMLDivElement> {
  kind: "success" | "error" | "info" | "warn";
  message: string;
  onDismiss?: () => void;
  duration?: number;
}

const Toast = React.forwardRef<HTMLDivElement, ToastProps & { "data-testid"?: string }>(
  ({ kind, message, onDismiss, duration = 4000, className, ...props }, ref) => {
    useEffect(() => {
      if (duration && onDismiss) {
        const timer = setTimeout(onDismiss, duration);
        return () => clearTimeout(timer);
      }
    }, [duration, onDismiss]);

    const iconMap = {
      success: "check",
      error: "close",
      info: "info",
      warn: "warn",
    } as const;

    return (
      <div
        ref={ref}
        className={`toast toast-${kind} ${className || ""}`.trim()}
        role="status"
        aria-live="polite"
        {...props}
      >
        <Icon name={iconMap[kind]} size={20} aria-hidden />
        <span className="toast-message">{message}</span>
        {onDismiss && (
          <button
            className="toast-close"
            onClick={onDismiss}
            aria-label="Dismiss notification"
          >
            <Icon name="close" size={16} aria-hidden />
          </button>
        )}
      </div>
    );
  }
);

Toast.displayName = "Toast";

export default Toast;
