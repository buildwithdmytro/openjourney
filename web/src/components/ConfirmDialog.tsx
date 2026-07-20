import React, { useState } from "react";
import Modal from "./Modal";
import Button from "./Button";

export interface ConfirmDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void | Promise<void>;
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  isDangerous?: boolean;
}

const ConfirmDialog = React.forwardRef<HTMLDivElement, ConfirmDialogProps>(
  (
    {
      isOpen,
      onClose,
      onConfirm,
      title,
      message,
      confirmText = "Confirm",
      cancelText = "Cancel",
      isDangerous = false,
    },
    ref
  ) => {
    const [isLoading, setIsLoading] = useState(false);

    const handleConfirm = async () => {
      setIsLoading(true);
      try {
        await onConfirm();
      } catch (error) {
        // Error is caught but not re-thrown to allow onClose to be called
        console.error("Confirm dialog error:", error);
      } finally {
        setIsLoading(false);
        onClose();
      }
    };

    return (
      <Modal
        ref={ref}
        isOpen={isOpen}
        onClose={onClose}
        aria-labelledby="confirm-dialog-title"
      >
        <div className="confirm-dialog">
          <h2 id="confirm-dialog-title">{title}</h2>
          <p>{message}</p>
          <div className="confirm-dialog-actions">
            <Button variant="secondary" onClick={onClose} disabled={isLoading}>
              {cancelText}
            </Button>
            <Button
              variant={isDangerous ? "danger" : "primary"}
              onClick={handleConfirm}
              disabled={isLoading}
            >
              {isLoading ? "..." : confirmText}
            </Button>
          </div>
        </div>
      </Modal>
    );
  }
);

ConfirmDialog.displayName = "ConfirmDialog";

export default ConfirmDialog;
