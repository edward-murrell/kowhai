// Label colours. Each has a background and a readable text colour.
export const LABEL_COLOURS: Record<string, { bg: string; fg: string }> = {
  green: { bg: '#4bce97', fg: '#09321f' },
  yellow: { bg: '#f5cd47', fg: '#3d2e00' },
  orange: { bg: '#fea362', fg: '#3d1a00' },
  red: { bg: '#f87168', fg: '#3d0a05' },
  purple: { bg: '#9f8fef', fg: '#1d1240' },
  blue: { bg: '#579dff', fg: '#071d40' },
  sky: { bg: '#6cc3e0', fg: '#032833' },
  lime: { bg: '#94c748', fg: '#1a2c05' },
  pink: { bg: '#e774bb', fg: '#3d0b2a' },
  black: { bg: '#8590a2', fg: '#101214' },
}

export function labelStyle(colour: string): Record<string, string> {
  const c = LABEL_COLOURS[colour] ?? { bg: colour, fg: '#111' }
  return { backgroundColor: c.bg, color: c.fg }
}

// Board backgrounds.
export const BACKGROUNDS: Record<string, string> = {
  kowhai: 'linear-gradient(135deg, #f5b700 0%, #d98e04 100%)',
  bush: 'linear-gradient(135deg, #2f5d3a 0%, #1f3b27 100%)',
  sea: 'linear-gradient(135deg, #2b7a9b 0%, #174a63 100%)',
  dusk: 'linear-gradient(135deg, #6b4c9a 0%, #2e2350 100%)',
  sand: 'linear-gradient(135deg, #e8d5b0 0%, #c9ad7a 100%)',
  slate: 'linear-gradient(135deg, #4a5568 0%, #2d3748 100%)',
}

export function boardBackground(bg: string): string {
  return BACKGROUNDS[bg] ?? BACKGROUNDS.bush
}
