import React, { useEffect, useRef, ReactNode } from "react";
import { createPortal } from "react-dom";

export interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  children: ReactNode;
  "aria-label"?: string;
  "aria-labelledby"?: string;
}

const Modal = React.forwardRef<HTMLDivElement, ModalProps>(
  ({ isOpen, onClose, children, "aria-label": ariaLabel, "aria-labelledby": ariaLabelledby }, ref) => {
    const dialogRef = useRef<HTMLDivElement>(null);
    const previousActiveElementRef = useRef<HTMLElement | null>(null);

    useEffect(() => {
      if (!isOpen) return;

      // Capture the element that had focus before the modal opened
      previousActiveElementRef.current = document.activeElement as HTMLElement;

      // Get all focusable elements within the dialog
      const getFocusableElements = () => {
        const dialog = dialogRef.current;
        if (!dialog) return [];
        return Array.from(
          dialog.querySelectorAll(
            'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
          )
        ) as HTMLElement[];
      };

      // Focus the first focusable element in the dialog
      const focusableElements = getFocusableElements();
      if (focusableElements.length > 0) {
        focusableElements[0].focus();
      }

      // Handle keyboard events
      const handleKeydown = (event: KeyboardEvent) => {
        // Close on Escape
        if (event.key === "Escape") {
          event.preventDefault();
          onClose();
          return;
        }

        // Trap Tab within the dialog
        if (event.key === "Tab") {
          const focusableElements = getFocusableElements();
          if (focusableElements.length === 0) return;

          const focusedElement = document.activeElement;
          const firstElement = focusableElements[0];
          const lastElement = focusableElements[focusableElements.length - 1];

          if (event.shiftKey) {
            // Shift+Tab
            if (focusedElement === firstElement) {
              event.preventDefault();
              lastElement.focus();
            }
          } else {
            // Tab
            if (focusedElement === lastElement) {
              event.preventDefault();
              firstElement.focus();
            }
          }
        }
      };

      document.addEventListener("keydown", handleKeydown);

      // Restore focus when modal closes
      return () => {
        document.removeEventListener("keydown", handleKeydown);
        if (previousActiveElementRef.current && previousActiveElementRef.current.focus) {
          previousActiveElementRef.current.focus();
        }
      };
    }, [isOpen, onClose]);

    if (!isOpen) return null;

    const portalRoot = document.getElementById("modal-root");
    if (!portalRoot) return null;

    return createPortal(
      <div
        className="modal-backdrop"
        onClick={(e) => {
          if (e.target === e.currentTarget) {
            onClose();
          }
        }}
      >
        <div
          ref={ref || dialogRef}
          className="modal-content"
          role="dialog"
          aria-modal="true"
          aria-label={ariaLabel}
          aria-labelledby={ariaLabelledby}
        >
          {children}
        </div>
      </div>,
      portalRoot
    );
  }
);

Modal.displayName = "Modal";

export default Modal;
