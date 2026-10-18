<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PrinterInfo } from '../api'

type TagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, te } = useI18n()

const types: Record<string, TagType> = {
  RUNNING: 'primary',
  PREPARE: 'warning',
  PAUSE: 'warning',
  FINISH: 'success',
  FAILED: 'danger',
}

const type = computed<TagType>(() => (props.printer.online ? (types[props.printer.status.state] ?? 'info') : 'info'))
const label = computed(() => {
  if (!props.printer.online) {
    return t('state.OFFLINE')
  }
  const key = `state.${props.printer.status.state}`
  return te(key) ? t(key) : t('state.unknown')
})
</script>

<template>
  <el-tag :type="type" effect="dark" size="small">{{ label }}</el-tag>
</template>
