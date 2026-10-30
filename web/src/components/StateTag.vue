<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PrinterInfo } from '../api'
import { STATE_ICONS, STATE_TYPES, stateKey } from '../format'

const props = defineProps<{ printer: PrinterInfo }>()
const { t } = useI18n()

const key = computed(() => stateKey(props.printer))
</script>

<template>
  <el-tag :type="STATE_TYPES[key] ?? 'info'" effect="dark" size="small">
    <fa-icon :icon="STATE_ICONS[key]" class="ic" />{{ t(`state.${key}`) }}
  </el-tag>
</template>
