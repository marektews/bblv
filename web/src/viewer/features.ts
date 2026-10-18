import type { Locale } from '../i18n'

// Colours and translations of the feature names Bambu Studio writes after "; FEATURE:".
const features: Record<string, { color: string; pl: string }> = {
  'Outer wall': { color: '#ff8c1a', pl: 'Ściana zewnętrzna' },
  'Inner wall': { color: '#ffd24d', pl: 'Ściana wewnętrzna' },
  'Overhang wall': { color: '#3d5cf2', pl: 'Ściana nawisu' },
  'Sparse infill': { color: '#b03029', pl: 'Wypełnienie' },
  'Internal solid infill': { color: '#9654cc', pl: 'Wewnętrzne wypełnienie pełne' },
  'Top surface': { color: '#f04060', pl: 'Powierzchnia górna' },
  'Bottom surface': { color: '#6b8e23', pl: 'Powierzchnia dolna' },
  Bridge: { color: '#4d80ba', pl: 'Most' },
  'Internal Bridge': { color: '#6fa3d9', pl: 'Most wewnętrzny' },
  'Gap infill': { color: '#e6e6e6', pl: 'Wypełnienie szczelin' },
  Support: { color: '#2fb84f', pl: 'Podpora' },
  'Support interface': { color: '#1f7a35', pl: 'Styk podpory' },
  'Support transition': { color: '#54d17a', pl: 'Przejście podpory' },
  'Prime tower': { color: '#a8a8a8', pl: 'Wieża czyszcząca' },
  Skirt: { color: '#00a0a0', pl: 'Obrys' },
  Brim: { color: '#00b3b3', pl: 'Brim' },
  Ironing: { color: '#ff9999', pl: 'Prasowanie' },
  Custom: { color: '#5ec2ff', pl: 'Własny G-code' },
}

const unknown = { color: '#888888', pl: 'Inne', en: 'Other' }

export function featureColor(name: string): string {
  if (!name) {
    return unknown.color
  }
  if (features[name]) {
    return features[name].color
  }
  // Stable colour for names this table does not know.
  let h = 0
  for (const c of name) {
    h = (h * 31 + c.charCodeAt(0)) % 360
  }
  return `hsl(${h}, 60%, 55%)`
}

export function featureLabel(name: string, locale: Locale): string {
  if (!name) {
    return locale === 'pl' ? unknown.pl : unknown.en
  }
  return locale === 'pl' ? (features[name]?.pl ?? name) : name
}
