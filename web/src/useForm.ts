import { useState, useCallback } from "react";

export interface UseFormOptions<T extends Record<string, any>> {
  initialValues: T;
  validate?: Partial<Record<keyof T, (value: any) => string | undefined>>;
}

export interface UseFormReturn<T extends Record<string, any>> {
  values: T;
  errors: Partial<Record<keyof T, string>>;
  touched: Set<keyof T>;
  isValid: boolean;
  setValue: (key: keyof T, value: any) => void;
  touch: (key: keyof T) => void;
  reset: () => void;
  getError: (key: keyof T) => string | undefined;
  handleChange: (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => void;
  handleBlur: (e: React.FocusEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => void;
}

export function useForm<T extends Record<string, any>>(
  options: UseFormOptions<T>
): UseFormReturn<T> {
  const { initialValues, validate = {} } = options;

  const validateAll = (vals: T): Partial<Record<keyof T, string>> => {
    const newErrors: Partial<Record<keyof T, string>> = {};
    (Object.keys(validate) as Array<keyof T>).forEach((k) => {
      const validator = (validate as Record<keyof T, any>)[k];
      if (validator) {
        const error = validator(vals[k]);
        if (error) {
          newErrors[k] = error;
        }
      }
    });
    return newErrors;
  };

  const [values, setValues] = useState<T>(initialValues);
  const [touched, setTouched] = useState<Set<keyof T>>(new Set());
  const [errors, setErrors] = useState<Partial<Record<keyof T, string>>>(() =>
    validateAll(initialValues)
  );

  const validateField = useCallback(
    (key: keyof T, value: any): string | undefined => {
      const validator = (validate as Record<keyof T, any>)[key];
      if (validator) {
        return validator(value);
      }
      return undefined;
    },
    [validate]
  );

  const setValue = useCallback((key: keyof T, value: any) => {
    setValues((prev) => {
      const updated = { ...prev, [key]: value };
      const error = validateField(key, value);
      setErrors((prevErrors) => {
        const newErrors = { ...prevErrors };
        if (error) {
          newErrors[key] = error;
        } else {
          delete newErrors[key];
        }
        return newErrors;
      });
      return updated;
    });
  }, [validateField]);

  const touch = useCallback((key: keyof T) => {
    setTouched((prev) => new Set([...prev, key]));
  }, []);

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => {
      const { name, value, type } = e.target;
      const key = name as keyof T;
      let finalValue: any = value;
      if (type === "checkbox") {
        finalValue = (e.target as HTMLInputElement).checked;
      } else if (type === "number") {
        finalValue = value ? Number(value) : "";
      }
      setValue(key, finalValue);
    },
    [setValue]
  );

  const handleBlur = useCallback(
    (e: React.FocusEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => {
      const { name } = e.target;
      const key = name as keyof T;
      touch(key);
    },
    [touch]
  );

  const reset = useCallback(() => {
    setValues(initialValues);
    setTouched(new Set());
    setErrors({});
  }, [initialValues]);

  const getError = useCallback(
    (key: keyof T): string | undefined => {
      if (!touched.has(key)) return undefined;
      return errors[key];
    },
    [errors, touched]
  );

  const isValid = Object.keys(errors).length === 0;

  return {
    values,
    errors,
    touched,
    isValid,
    setValue,
    touch,
    reset,
    getError,
    handleChange,
    handleBlur,
  };
}
