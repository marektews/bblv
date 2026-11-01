<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { alertKey, dismissAlerts, getAlerts, restoreAlerts, type Alert, type PrinterInfo } from '../api'
import { errorCode, statusAlertKeys } from '../format'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, locale } = useI18n()
const alerts = ref<Alert[] | null>(null) // null until loaded or when loading failed

// The descriptions only change with the codes or the language.
const key = computed(() => [props.printer.id, locale.value, props.printer.status.printError, ...props.printer.status.hms].join('|'))
const any = computed(() => props.printer.status.printError !== 0 || props.printer.status.hms.length > 0)

let request = 0
watch(
  key,
  async () => {
    const n = ++request
    if (!any.value) {
      alerts.value = []
      return
    }
    try {
      const list = await getAlerts(props.printer.id, locale.value)
      if (n === request) {
        alerts.value = list
      }
    } catch {
      if (n === request) {
        alerts.value = null
      }
    }
  },
  { immediate: true },
)

// Alerts someone has read are hidden for everyone until they go away; the
// hidden ones can be shown, and brought back.
const dismissed = computed(() => new Set(props.printer.dismissedAlerts ?? []))
const isHidden = (k: string) => dismissed.value.has(k)
const keys = computed(() => statusAlertKeys(props.printer.status))
const visibleKeys = computed(() => keys.value.filter((k) => !isHidden(k)))
const hiddenCount = computed(() => keys.value.length - visibleKeys.value.length)
const showHidden = ref(false)
const shown = computed(() => (alerts.value ?? []).filter((a) => showHidden.value || !isHidden(alertKey(a.kind, a.code))))
const busy = ref(false)
// A closed el-alert hides itself; a failed request brings it back.
const generation = ref(0)

watch(hiddenCount, (n) => {
  if (n === 0) {
    showHidden.value = false
  }
})

async function change(f: typeof dismissAlerts, list: string[]) {
  busy.value = true
  try {
    await f(props.printer.id, list)
  } catch (err) {
    generation.value++
    ElMessage.error(t('status.alertChangeFailed', { msg: err instanceof Error ? err.message : String(err) }))
  } finally {
    busy.value = false
  }
}

const types: Record<string, 'error' | 'warning' | 'info'> = { fatal: 'error', serious: 'error', common: 'warning', info: 'info' }
const typeOf = (a: Alert) => (a.kind === 'error' ? 'error' : (types[a.severity ?? ''] ?? 'warning'))
const title = (a: Alert) => (a.kind === 'error' ? `${t('status.printError')} ${a.code}` : `HMS ${a.code}`)
</script>

<template>
  <div v-if="!any" class="muted">{{ t('status.noAlerts') }}</div>
  <template v-else>
    <div class="bar">
      <span v-if="visibleKeys.length === 0" class="muted">{{ t('status.allDismissed') }}</span>
      <el-button v-if="visibleKeys.length > 0" link size="small" :loading="busy" @click="change(dismissAlerts, visibleKeys)">
        <fa-icon icon="xmark" class="ic" />{{ t('status.dismissAll') }}
      </el-button>
      <el-button v-if="hiddenCount > 0" link size="small" @click="showHidden = !showHidden">
        <fa-icon :icon="showHidden ? 'eye-slash' : 'eye'" class="ic" />{{
          showHidden ? t('status.hideDismissed') : t('status.showDismissed', { n: hiddenCount })
        }}
      </el-button>
    </div>
    <div v-if="alerts" class="alerts">
      <el-alert
        v-for="a in shown"
        :key="`${a.kind}${a.code}${generation}`"
        :type="typeOf(a)"
        :title="title(a)"
        :closable="!isHidden(alertKey(a.kind, a.code))"
        :class="{ dismissed: isHidden(alertKey(a.kind, a.code)) }"
        show-icon
        @close="change(dismissAlerts, [alertKey(a.kind, a.code)])"
      >
        <p v-if="a.text" class="text">{{ a.text }}</p>
        <div class="links">
          <a :href="a.url" target="_blank" rel="noopener noreferrer">
            {{ t('status.moreInfo') }}<fa-icon icon="arrow-up-right-from-square" class="ic-after" />
          </a>
          <el-button v-if="isHidden(alertKey(a.kind, a.code))" link size="small" @click="change(restoreAlerts, [alertKey(a.kind, a.code)])">
            {{ t('status.restoreAlert') }}
          </el-button>
        </div>
      </el-alert>
    </div>
    <div v-else class="codes">
      <el-tag
        v-if="printer.status.printError && (showHidden || !isHidden(alertKey('error', errorCode(printer.status.printError))))"
        type="danger"
        :closable="!isHidden(alertKey('error', errorCode(printer.status.printError)))"
        @close="change(dismissAlerts, [alertKey('error', errorCode(printer.status.printError))])"
      >
        <fa-icon icon="circle-exclamation" class="ic" />{{ t('status.printError') }} {{ errorCode(printer.status.printError) }}
      </el-tag>
      <template v-for="code in printer.status.hms" :key="code">
        <el-tag
          v-if="showHidden || !isHidden(alertKey('hms', code))"
          type="warning"
          :closable="!isHidden(alertKey('hms', code))"
          @close="change(dismissAlerts, [alertKey('hms', code)])"
        >
          <fa-icon icon="triangle-exclamation" class="ic" />HMS {{ code }}
        </el-tag>
      </template>
    </div>
  </template>
</template>

<style scoped>
.bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
  margin-bottom: 8px;
}

.bar .el-button + .el-button {
  margin-left: 0;
}

.alerts {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dismissed {
  opacity: 0.55;
}

.text {
  margin: 4px 0;
  line-height: 1.4;
}

.links {
  display: flex;
  align-items: center;
  gap: 12px;
}

.alerts a {
  color: var(--el-color-primary);
  font-size: 12px;
}

.codes {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
