import React, { ReactElement, ReactNode } from "react";

export interface FieldProps {
  id?: string;
  label?: ReactNode;
  help?: ReactNode;
  error?: ReactNode;
  required?: boolean;
  children: ReactElement;
  className?: string;
}

export default function Field({
  id: providedId,
  label,
  help,
  error,
  required,
  children,
  className,
}: FieldProps) {
  const id = providedId || `field-${Math.random().toString(36).slice(2, 9)}`;

  const describedBy = [help && `${id}-help`, error && `${id}-error`]
    .filter(Boolean)
    .join(" ");

  const child = React.cloneElement(children, {
    id,
    ...(describedBy && { "aria-describedby": describedBy }),
    ...(error && { "aria-invalid": true }),
  } as React.InputHTMLAttributes<HTMLInputElement>);

  return (
    <div className={`field ${className || ""}`.trim()}>
      {label && (
        <label htmlFor={id} className="field-label">
          {label}
          {required && <span className="required" aria-label="required">*</span>}
        </label>
      )}
      {child}
      {help && (
        <div id={`${id}-help`} className="field-help">
          {help}
        </div>
      )}
      {error && (
        <div id={`${id}-error`} className="field-error">
          {error}
        </div>
      )}
    </div>
  );
}
