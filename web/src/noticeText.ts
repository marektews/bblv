import type { Notice, NoticeKind } from './api'
import { i18n } from './i18n'

export interface NoticeText {
  kind: NoticeKind // the most important kind selected
  title: string
  body: string
}

// textOf puts a notice into words for the kinds selected, null when none
// of its kinds is; alerts are named by their codes and, when given, their
// descriptions (code → text).
export function textOf(n: Notice, selected: NoticeKind[], descriptions: Map<string, string> = new Map()): NoticeText | null {
  const kind = n.kinds.find((k) => selected.includes(k))
  if (!kind) {
    return null
  }
  const t = i18n.global.t
  const lines = n.subtask ? [n.subtask] : []
  for (const c of n.codes ?? []) {
    const label = c.kind === 'error' ? t('notify.error') : 'HMS'
    const text = descriptions.get(c.code)
    lines.push(text ? `${label} ${c.code}: ${text}` : `${label} ${c.code}`)
  }
  return { kind, title: t('notify.title', { printer: n.printerName, what: t(`notify.kinds.${kind}`) }), body: lines.join('\n') }
}
