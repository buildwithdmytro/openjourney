import React from "react";

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  error?: string;
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ error, className, ...props }, ref) => {
    return (
      <input
        ref={ref}
        className={`input ${className || ""}`.trim()}
        {...(error ? { "aria-invalid": "true" } : {})}
        {...props}
      />
    );
  }
);

Input.displayName = "Input";

export default Input;
