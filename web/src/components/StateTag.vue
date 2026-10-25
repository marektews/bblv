<script setup lang="ts">
import type { IconName } from '@fortawesome/fontawesome-svg-core'
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

const icons: Record<string, IconName> = {
  IDLE: 'moon',
  PREPARE: 'gears',
  RUNNING: 'print',
  PAUSE: 'pause',
  FINISH: 'circle-check',
  FAILED: 'circle-xmark',
}

const type = computed<TagType>(() => (props.printer.online ? (types[props.printer.status.state] ?? 'info') : 'info'))
const icon = computed<IconName>(() => (props.printer.online ? (icons[props.printer.status.state] ?? 'circle-question') : 'plug-circle-xmark'))
const label = computed(() => {
  if (!props.printer.online) {
    return t('state.OFFLINE')
  }
  const key = `state.${props.printer.status.state}`
  return te(key) ? t(key) : t('state.unknown')
})
</script>

<template>
  <el-tag :type="type" effect="dark" size="small"><fa-icon :icon="icon" class="ic" />{{ label }}</el-tag>
</template>
