import React from "react";

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "danger" | "ghost";
  size?: "sm" | "md";
  loading?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { variant = "primary", size = "md", loading, className, children, disabled, ...props },
    ref
  ) => {
    const variantClass =
      variant === "primary"
        ? "btn-primary"
        : variant === "secondary"
          ? "btn-secondary"
          : variant === "danger"
            ? "btn-danger"
            : "btn-ghost";

    const sizeClass = size === "sm" ? "btn-sm" : "btn-md";

    return (
      <button
        ref={ref}
        className={`btn ${variantClass} ${sizeClass} ${className || ""}`.trim()}
        disabled={disabled || loading}
        {...props}
      >
        {children}
      </button>
    );
  }
);

Button.displayName = "Button";

export default Button;
