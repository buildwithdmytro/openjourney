import React from "react";

export interface SpinnerProps extends React.HTMLAttributes<HTMLDivElement> {
  size?: "sm" | "md" | "lg";
  label?: string;
}

const Spinner = React.forwardRef<HTMLDivElement, SpinnerProps>(
  ({ size = "md", label = "Loading", className, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={`spinner spinner-${size} ${className || ""}`.trim()}
        role="status"
        aria-label={label}
        {...props}
      />
    );
  }
);

Spinner.displayName = "Spinner";

export default Spinner;
