import React from "react";

export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  error?: string;
}

const Textarea = React.forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ error, className, ...props }, ref) => {
    return (
      <textarea
        ref={ref}
        className={`textarea ${className || ""}`.trim()}
        {...(error ? { "aria-invalid": "true" } : {})}
        {...props}
      />
    );
  }
);

Textarea.displayName = "Textarea";

export default Textarea;
