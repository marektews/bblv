<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAlerts, type Alert, type PrinterInfo } from '../api'
import { errorCode } from '../format'

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

const types: Record<string, 'error' | 'warning' | 'info'> = { fatal: 'error', serious: 'error', common: 'warning', info: 'info' }
const typeOf = (a: Alert) => (a.kind === 'error' ? 'error' : (types[a.severity ?? ''] ?? 'warning'))
const title = (a: Alert) => (a.kind === 'error' ? `${t('status.printError')} ${a.code}` : `HMS ${a.code}`)
</script>

<template>
  <div v-if="!any" class="muted">{{ t('status.noAlerts') }}</div>
  <div v-else-if="alerts" class="alerts">
    <el-alert v-for="a in alerts" :key="a.kind + a.code" :type="typeOf(a)" :title="title(a)" :closable="false" show-icon>
      <p v-if="a.text" class="text">{{ a.text }}</p>
      <a :href="a.url" target="_blank" rel="noopener noreferrer">
        {{ t('status.moreInfo') }}<fa-icon icon="arrow-up-right-from-square" class="ic-after" />
      </a>
    </el-alert>
  </div>
  <div v-else class="codes">
    <el-tag v-if="printer.status.printError" type="danger">
      <fa-icon icon="circle-exclamation" class="ic" />{{ t('status.printError') }} {{ errorCode(printer.status.printError) }}
    </el-tag>
    <el-tag v-for="code in printer.status.hms" :key="code" type="warning">
      <fa-icon icon="triangle-exclamation" class="ic" />HMS {{ code }}
    </el-tag>
  </div>
</template>

<style scoped>
.alerts {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.text {
  margin: 4px 0;
  line-height: 1.4;
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
