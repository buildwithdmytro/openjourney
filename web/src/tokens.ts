/* Design tokens as TypeScript constants for use in component code */

/* Default color values for template composer (match token defaults) */
export const defaultAccentColor = "#6f5cff"; /* matches var(--color-accent) in light mode */
export const defaultBackgroundColor = "#f4f6f8"; /* matches var(--color-surface-muted) */

/* Journey and status-specific colors for runtime use */
export const journeyColors = {
  successBorder: "#187d56",
  successBg: "#e9f8f1",
  successText: "#187d56",
  errorBorder: "#d93025",
  errorBg: "#fce8e6",
  errorText: "#c5221f",
  infoBorder: "#1a73e8",
  infoBg: "#e8f0fe",
  infoText: "#1a73e8",
  purpleBorder: "#af52de",
  purpleBg: "#f5ecfc",
  purpleText: "#af52de",
  orangeBorder: "#e27220",
  orangeBg: "#fef3eb",
  orangeText: "#e27220",
  neutralBorder: "#5f6368",
  neutralBg: "#f1f3f4",
  neutralText: "#5f6368",
  white: "#fff",
  black: "#222",
  errorMessageBg: "#fdf6f6",
  errorMessageBorder: "#f5c6cb",
  errorMessageText: "#721c24",
  selectedRowBg: "#f1f3f4",
  dotBg: "#1a73e8",
  codeBg: "#f8f9fa",
  errorAlertBorder: "#dadce0",
  canvasGridColor: "#d9ddea",
  danger: "#a93838",
  dangerBg: "#fff0f0",
};

export const tokens = {
  /* Colors: surface & ink */
  colorSurfaceDefault: "var(--color-surface-default)",
  colorSurfaceMuted: "var(--color-surface-muted)",
  colorSurfaceSubtle: "var(--color-surface-subtle)",
  colorInk: "var(--color-ink)",
  colorInkMuted: "var(--color-ink-muted)",
  colorInkSubtle: "var(--color-ink-subtle)",

  /* Colors: borders */
  colorBorderDefault: "var(--color-border-default)",
  colorBorderSubtle: "var(--color-border-subtle)",
  colorBorderMuted: "var(--color-border-muted)",
  colorBorderLight: "var(--color-border-light)",
  colorBorderLighter: "var(--color-border-lighter)",

  /* Colors: functional */
  colorAccent: "var(--color-accent)",
  colorAccentDark: "var(--color-accent-dark)",
  colorAccentLight: "var(--color-accent-light)",
  colorDanger: "var(--color-danger)",
  colorDangerBg: "var(--color-danger-bg)",
  colorDangerText: "var(--color-danger-text)",
  colorSuccess: "var(--color-success)",
  colorSuccessBg: "var(--color-success-bg)",
  colorSuccessText: "var(--color-success-text)",
  colorSuccessLight: "var(--color-success-light)",
  colorWarn: "var(--color-warn)",
  colorWarnBg: "var(--color-warn-bg)",
  colorWarnText: "var(--color-warn-text)",
  colorInfo: "var(--color-info)",

  /* Space scale (4px base) */
  spaceXs: "var(--space-xs)",
  spaceSm: "var(--space-sm)",
  spaceMd: "var(--space-md)",
  spaceLg: "var(--space-lg)",
  spaceXl: "var(--space-xl)",
  space2xl: "var(--space-2xl)",
  space3xl: "var(--space-3xl)",
  space4xl: "var(--space-4xl)",
  space5xl: "var(--space-5xl)",

  /* Radius */
  radiusSm: "var(--radius-sm)",
  radiusMd: "var(--radius-md)",
  radiusLg: "var(--radius-lg)",
  radiusXl: "var(--radius-xl)",
  radius2xl: "var(--radius-2xl)",
  radiusFull: "var(--radius-full)",

  /* Shadows */
  shadowSm: "var(--shadow-sm)",
  shadowMd: "var(--shadow-md)",
  shadowLg: "var(--shadow-lg)",
  shadowXl: "var(--shadow-xl)",
  shadow2xl: "var(--shadow-2xl)",
  shadowInset: "var(--shadow-inset)",

  /* Typography */
  fontFamily: "var(--font-family)",
  fontMono: "var(--font-mono)",

  /* Motion */
  motionFast: "var(--motion-fast)",
  motionNormal: "var(--motion-normal)",
};

/* Static color values for runtime use (e.g., StatusBadge) */
export const staticColors = {
  light: {
    success: "#187d56",
    successBg: "#e9f8f1",
    info: "#1a73e8",
    infoBg: "#e8f0fe",
    warn: "#b06000",
    warnBg: "#fff8df",
    neutral: "#5f6368",
    neutralBg: "#f1f3f4",
    danger: "#a93838",
    dangerBg: "#fff0f0",
    default: "#202124",
    defaultBg: "#f8f9fa",
    defaultBorder: "#dadce0",
  },
  dark: {
    success: "#78dbb4",
    successBg: "#183c34",
    info: "#8ab4f8",
    infoBg: "#1b2f4b",
    warn: "#ffd27a",
    warnBg: "#46391d",
    neutral: "#ccc",
    neutralBg: "#2d2d2d",
    danger: "#ff8a9e",
    dangerBg: "#4a1f2a",
    default: "#e8e8e8",
    defaultBg: "#2d2d2d",
    defaultBorder: "#4a4a4a",
  },
};
