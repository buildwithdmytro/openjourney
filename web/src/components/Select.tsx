import React from "react";

export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  error?: string;
  options?: Array<{ value: string | number; label: string }>;
}

const Select = React.forwardRef<HTMLSelectElement, SelectProps>(
  ({ error, options, className, children, ...props }, ref) => {
    return (
      <select
        ref={ref}
        className={`select ${className || ""}`.trim()}
        {...(error ? { "aria-invalid": "true" } : {})}
        {...props}
      >
        {options
          ? options.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))
          : children}
      </select>
    );
  }
);

Select.displayName = "Select";

export default Select;
