import { ElNotification } from 'element-plus'
import { h, reactive, watch } from 'vue'
import { getAlerts, NOTICE_KINDS, type Notice, type NoticeKind } from './api'
import { i18n } from './i18n'
import { textOf, type NoticeText } from './noticeText'

// navigate opens a page of the app; main.ts sets the router's, which this
// module cannot import without a cycle through the views and the store.
let navigate = (path: string) => window.location.assign(path)

export function setNavigator(f: (path: string) => void) {
  navigate = f
}

// What this browser shows; every device agrees to notifications itself.
const STORAGE_KEY = 'notifications'

export const browserNotify = reactive({ enabled: false, kinds: [...NOTICE_KINDS] as NoticeKind[] })

try {
  const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null')
  if (saved) {
    browserNotify.enabled = saved.enabled === true
    if (Array.isArray(saved.kinds)) {
      browserNotify.kinds = NOTICE_KINDS.filter((k) => saved.kinds.includes(k))
    }
  }
} catch {
  // Nothing saved or no storage: notifications stay off.
}
watch(browserNotify, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(v))
  } catch {
    // Not remembered.
  }
})

// Whether this browser can show system notifications. Chrome on Android
// shows them only through a service worker, which a self-signed
// certificate keeps from running; iPhones only for apps on the home screen.
export function systemSupport(): 'yes' | 'no' | 'mobile' {
  if (!('Notification' in window)) {
    return /iPhone|iPad/.test(navigator.userAgent) ? 'mobile' : 'no'
  }
  return /Android/.test(navigator.userAgent) ? 'mobile' : 'yes'
}

// enable asks for the permission and turns the notifications on if granted.
export async function enable(): Promise<NotificationPermission | 'unsupported'> {
  if (systemSupport() !== 'yes') {
    // The page itself still shows them while it is visible.
    browserNotify.enabled = true
    return 'unsupported'
  }
  const permission = Notification.permission === 'default' ? await Notification.requestPermission() : Notification.permission
  browserNotify.enabled = permission === 'granted'
  return permission
}

// Tabs tell each other which notices they showed on the page, so that a
// tab in the background does not show the same one as a system
// notification too.
const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('bblv-notifications')
const shownElsewhere = new Set<number>()
if (channel) {
  channel.onmessage = (e: MessageEvent<number>) => shownElsewhere.add(e.data)
}

function open(printerId: string) {
  window.focus()
  navigate(printerId ? `/printer/${encodeURIComponent(printerId)}` : '/')
}

function system(text: NoticeText, tag: string, printerId: string) {
  try {
    const n = new Notification(text.title, { body: text.body, tag, icon: '/favicon.svg' })
    n.onclick = () => {
      open(printerId)
      n.close()
    }
  } catch {
    // Browsers without the constructor show nothing in the background.
  }
}

const pageType: Record<NoticeKind, 'error' | 'warning' | 'success'> = { failed: 'error', pause: 'warning', finish: 'success', hms: 'warning' }

// show tells about a notice the way this browser is set to: on the page
// while it is visible, otherwise as a system notification.
export async function show(n: Notice) {
  if (!browserNotify.enabled || !n.kinds.some((k) => browserNotify.kinds.includes(k))) {
    return
  }
  let descriptions = new Map<string, string>()
  if (n.codes?.length) {
    try {
      const alerts = await getAlerts(n.printerId, i18n.global.locale.value)
      descriptions = new Map(alerts.map((a) => [a.code, a.text]))
    } catch {
      // The codes alone then.
    }
  }
  const text = textOf(n, browserNotify.kinds, descriptions)
  if (!text) {
    return
  }
  if (document.visibilityState === 'visible') {
    channel?.postMessage(n.id)
    ElNotification({
      title: text.title,
      // One line each; as text, since the print's name comes from the printer.
      message: h('div', text.body.split('\n').map((line) => h('div', line))),
      type: pageType[text.kind],
      duration: 15000,
      onClick: () => open(n.printerId),
    })
    return
  }
  if (systemSupport() !== 'yes' || Notification.permission !== 'granted') {
    return
  }
  // A visible tab announces its own a moment later.
  await new Promise((resolve) => setTimeout(resolve, 500))
  if (!shownElsewhere.delete(n.id)) {
    system(text, `bblv-${n.id}`, n.printerId)
  }
}

// showTest shows a system notification at once, to check that this
// browser lets the page show them.
export function showTest(): boolean {
  if (systemSupport() !== 'yes' || Notification.permission !== 'granted') {
    return false
  }
  const t = i18n.global.t
  system({ kind: 'finish', title: t('notify.testTitle'), body: t('notify.testBody') }, 'bblv-test', '')
  return true
}
