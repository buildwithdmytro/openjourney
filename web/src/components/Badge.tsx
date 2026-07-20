import React from "react";

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  kind?:
    | "default"
    | "success"
    | "warn"
    | "danger"
    | "draft"
    | "published"
    | "active"
    | "completed"
    | "paused"
    | "waiting"
    | "subscribed";
}

const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  ({ kind = "default", className, children, ...props }, ref) => {
    const kindClass =
      kind === "success" || kind === "subscribed"
        ? "pill subscribed"
        : kind === "draft"
          ? "pill draft"
          : kind === "published" || kind === "active" || kind === "completed"
            ? "pill published"
            : kind === "paused" || kind === "waiting"
              ? "pill waiting"
              : "pill";

    return (
      <span
        ref={ref}
        className={`${kindClass} ${className || ""}`.trim()}
        {...props}
      >
        {children}
      </span>
    );
  }
);

Badge.displayName = "Badge";

export default Badge;
