import React, { useState } from "react";

export interface JsonFieldProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  error?: string;
  validateOnBlur?: boolean;
}

const JsonField = React.forwardRef<HTMLTextAreaElement, JsonFieldProps>(
  ({ value = "", onChange, onBlur, error, validateOnBlur = true, className, ...props }, ref) => {
    const [localError, setLocalError] = useState<string | undefined>(undefined);

    const handleBlur = (e: React.FocusEvent<HTMLTextAreaElement>) => {
      if (validateOnBlur && value) {
        try {
          JSON.parse(String(value));
          setLocalError(undefined);
        } catch (err) {
          setLocalError(`Invalid JSON: ${err instanceof Error ? err.message : 'Parse error'}`);
        }
      }
      onBlur?.(e);
    };

    const handleFormat = () => {
      try {
        const parsed = JSON.parse(String(value));
        const formatted = JSON.stringify(parsed, null, 2);
        const event = new Event('change', { bubbles: true });
        Object.defineProperty(event, 'target', {
          writable: false,
          value: { value: formatted, name: props.name },
        });
        onChange?.(event as unknown as React.ChangeEvent<HTMLTextAreaElement>);
        setLocalError(undefined);
      } catch (err) {
        setLocalError(`Invalid JSON: ${err instanceof Error ? err.message : 'Parse error'}`);
      }
    };

    const displayError = error || localError;

    return (
      <div className="json-field-wrapper">
        <textarea
          ref={ref}
          className={`textarea json-field ${className || ""}`.trim()}
          value={value}
          onChange={onChange}
          onBlur={handleBlur}
          {...(displayError ? { "aria-invalid": "true" } : {})}
          {...props}
        />
        <button
          type="button"
          className="json-format-btn"
          onClick={handleFormat}
          title="Pretty-print JSON"
          aria-label="Format JSON"
        >
          Format
        </button>
        {displayError && (
          <div className="field-error" role="alert">
            {displayError}
          </div>
        )}
      </div>
    );
  }
);

JsonField.displayName = "JsonField";

export default JsonField;
