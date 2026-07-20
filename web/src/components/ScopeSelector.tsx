import React, { useEffect, useRef, useState } from "react";

export interface ScopeSelectorProps {
  selected: string[];
  onChange: (scopes: string[]) => void;
  availableScopes: string[];
}

const ScopeSelector = React.forwardRef<
  HTMLDivElement,
  ScopeSelectorProps
>(({ selected, onChange, availableScopes }, ref) => {
  const [open, setOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(event.target as Node)
      ) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && open) {
        event.preventDefault();
        setOpen(false);
        buttonRef.current?.focus();
      }
    }
    if (open) {
      document.addEventListener("keydown", handleKeyDown);
      return () => document.removeEventListener("keydown", handleKeyDown);
    }
  }, [open]);

  const toggleScope = (scope: string) => {
    if (selected.includes(scope)) {
      onChange(selected.filter((s) => s !== scope));
    } else {
      onChange([...selected, scope]);
    }
  };

  const handleOptionKeyDown = (
    event: React.KeyboardEvent<HTMLDivElement>,
    index: number
  ) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelectedIndex((index + 1) % availableScopes.length);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelectedIndex(
        (index - 1 + availableScopes.length) % availableScopes.length
      );
    } else if (event.key === " " || event.key === "Enter") {
      event.preventDefault();
      toggleScope(availableScopes[index]);
    }
  };

  return (
    <div className="scope-selector" ref={ref || dropdownRef}>
      <button
        ref={buttonRef}
        type="button"
        className="scope-selector-btn"
        onClick={() => setOpen(!open)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        {selected.length === 0 ? "Select scopes..." : selected.join(", ")}
      </button>
      {open && (
        <div className="scope-selector-dropdown" role="listbox">
          {availableScopes.map((scope, index) => (
            <div
              key={scope}
              role="option"
              aria-selected={
                selected.includes(scope) ? "true" : "false"
              }
              className={`scope-option ${
                selectedIndex === index ? "focused" : ""
              }`}
              onClick={() => toggleScope(scope)}
              onKeyDown={(e) => handleOptionKeyDown(e, index)}
              tabIndex={0}
            >
              <input
                type="checkbox"
                id={`scope-${scope}`}
                checked={selected.includes(scope)}
                onChange={() => toggleScope(scope)}
                aria-label={scope}
              />
              <label htmlFor={`scope-${scope}`}>
                <code>{scope}</code>
              </label>
            </div>
          ))}
        </div>
      )}
    </div>
  );
});

ScopeSelector.displayName = "ScopeSelector";

export default ScopeSelector;
