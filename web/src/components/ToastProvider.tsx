import React, { createContext, useCallback, useContext, useMemo, useState, ReactNode } from "react";
import Toast from "./Toast";

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

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const push = useCallback(
    (toast: Omit<ToastMessage, "id">) => {
      const id = Math.random().toString(36).substr(2, 9);
      const newToast: ToastMessage = { ...toast, id };
      setToasts((prev) => [...prev, newToast]);
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
          <Toast
            key={toast.id}
            kind={toast.kind}
            message={toast.message}
            duration={toast.duration}
            onDismiss={() => dismiss(toast.id)}
            data-testid={`toast-${toast.id}`}
          />
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
