import React, { useEffect, useRef, useState } from "react";
import Modal from "./Modal";
import { navGroups, View, viewTitles } from "../navigation";

interface PaletteItem {
  label: string;
  view?: View;
  action?: () => void;
  category: string;
}


export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onNavigate: (view: View) => void;
  currentView: View;
}

export const CommandPalette = React.forwardRef<HTMLDivElement, CommandPaletteProps>(
  ({ isOpen, onClose, onNavigate, currentView }, ref) => {
    const [query, setQuery] = useState("");
    const [selectedIndex, setSelectedIndex] = useState(0);
    const inputRef = useRef<HTMLInputElement>(null);

    const items: PaletteItem[] = [];
    navGroups.forEach((group) => {
      group.items.forEach((view) => {
        items.push({
          label: viewTitles[view as View][0],
          view: view as View,
          category: group.label,
        });
      });
    });

    const filteredItems = query
      ? items.filter((item) =>
          item.label.toLowerCase().includes(query.toLowerCase())
        )
      : items;

    useEffect(() => {
      setQuery("");
      setSelectedIndex(0);
      if (isOpen && inputRef.current) {
        inputRef.current.focus();
      }
    }, [isOpen]);

    const handleKeydown = (event: React.KeyboardEvent) => {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((prev) => (prev + 1) % filteredItems.length);
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((prev) =>
          prev === 0 ? filteredItems.length - 1 : prev - 1
        );
      } else if (event.key === "Enter") {
        event.preventDefault();
        const item = filteredItems[selectedIndex];
        if (item && item.view) {
          onNavigate(item.view);
          onClose();
        }
      }
    };

    return (
      <Modal
        isOpen={isOpen}
        onClose={onClose}
        aria-label="Command palette"
        ref={ref}
      >
        <div className="command-palette">
          <input
            ref={inputRef}
            type="text"
            placeholder="Search views... (⌘K)"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            onKeyDown={handleKeydown}
            className="command-palette-input"
          />
          <div className="command-palette-list" role="listbox">
            {filteredItems.length === 0 ? (
              <div className="command-palette-empty">No views found</div>
            ) : (
              filteredItems.map((item, index) => (
                <div
                  key={`${item.category}-${item.label}`}
                  className={`command-palette-item ${
                    index === selectedIndex ? "selected" : ""
                  }`}
                  role="option"
                  aria-selected={index === selectedIndex}
                  onClick={() => {
                    if (item.view) {
                      onNavigate(item.view);
                      onClose();
                    }
                  }}
                >
                  <div className="command-palette-item-content">
                    <div className="command-palette-item-label">{item.label}</div>
                    <div className="command-palette-item-category">
                      {item.category}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </Modal>
    );
  }
);

CommandPalette.displayName = "CommandPalette";
