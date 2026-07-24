import React, { createContext, useCallback, useContext, useMemo, useState, ReactNode } from "react";
import Toast from "./Toast";

const MAX_VISIBLE_TOASTS = 5;

export interface ToastMessage {
  id: string;
  kind: "success" | "error" | "info" | "warn";
  message: string;
  duration?: number;
}

interface ToastContextType {
  push: (toast: Omit<ToastMessage, "id">) => void;
  dismiss: (id: string) => void;
}

const ToastContext = createContext<ToastContextType | undefined>(undefined);

function ToastItem({ toast, dismiss }: { toast: ToastMessage; dismiss: (id: string) => void }) {
  const onDismiss = useCallback(() => dismiss(toast.id), [dismiss, toast.id]);

  return (
    <Toast
      kind={toast.kind}
      message={toast.message}
      duration={toast.duration}
      onDismiss={onDismiss}
      data-testid={`toast-${toast.id}`}
    />
  );
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const push = useCallback(
    (toast: Omit<ToastMessage, "id">) => {
      const id = Math.random().toString(36).substr(2, 9);
      const newToast: ToastMessage = { ...toast, id };
      setToasts((prev) => [...prev, newToast].slice(-MAX_VISIBLE_TOASTS));
    },
    []
  );

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const value = useMemo(() => ({ push, dismiss }), [push, dismiss]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-container" role="region" aria-label="Notifications" data-testid="toast-container">
        {toasts.map((toast) => (
          <ToastItem key={toast.id} toast={toast} dismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextType {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error("useToast must be used within a ToastProvider");
  }
  return context;
}
