import { renderHook, act } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { useTheme } from './useTheme';

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })));
});

it('initializes to light mode by default', () => {
  const { result } = renderHook(() => useTheme());
  expect(result.current.theme).toBe('light');
});

it('respects prefers-color-scheme on first mount', () => {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: true }));
  const { result } = renderHook(() => useTheme());
  expect(result.current.theme).toBe('dark');
});

it('sets data-theme attribute on document root', async () => {
  const { result } = renderHook(() => useTheme());

  await act(async () => {
    result.current.setTheme('dark');
  });

  expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
});

it('persists theme to localStorage', async () => {
  const { result } = renderHook(() => useTheme());

  await act(async () => {
    result.current.setTheme('dark');
  });

  expect(localStorage.getItem('openjourney-theme')).toBe('dark');
});

it('restores theme from localStorage on mount', () => {
  localStorage.setItem('openjourney-theme', 'dark');

  const { result } = renderHook(() => useTheme());

  expect(result.current.theme).toBe('dark');
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
});

it('toggles between light and dark themes', async () => {
  const { result } = renderHook(() => useTheme());

  expect(result.current.theme).toBe('light');

  await act(async () => {
    result.current.toggle();
  });

  expect(result.current.theme).toBe('dark');
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark');

  await act(async () => {
    result.current.toggle();
  });

  expect(result.current.theme).toBe('light');
  expect(document.documentElement.getAttribute('data-theme')).toBeNull();
});
