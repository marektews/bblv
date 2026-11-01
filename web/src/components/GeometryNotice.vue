<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, uploadModel, type PrinterInfo } from '../api'

const props = defineProps<{ printer: PrinterInfo }>()
const { t } = useI18n()
const input = ref<HTMLInputElement>()
const uploading = ref(false)

const geometry = computed(() => props.printer.geometry)
const alert = computed(() => {
  switch (geometry.value.state) {
    case 'loading':
      return { type: 'info' as const, text: t('geometry.loading') }
    case 'missing':
      // Without a card the printer keeps the print in memory the server cannot read.
      return { type: 'warning' as const, text: props.printer.status.sdCard ? t('geometry.missing') : t('geometry.missingNoSd') }
    case 'error':
      return { type: 'error' as const, text: t('geometry.error', { msg: geometry.value.error ?? '' }) }
    case 'none':
      return { type: 'info' as const, text: t('geometry.none') }
    default:
      return geometry.value.uploaded ? { type: 'success' as const, text: t('geometry.uploaded') } : null
  }
})

async function onFile(e: Event) {
  const el = e.target as HTMLInputElement
  const file = el.files?.[0]
  el.value = ''
  if (!file) {
    return
  }
  uploading.value = true
  try {
    await send(file, 0)
  } finally {
    uploading.value = false
  }
}

async function send(file: File, plate: number) {
  try {
    await uploadModel(props.printer.id, file, plate)
    ElMessage.success(t('geometry.uploadDone'))
  } catch (err) {
    if (err instanceof ApiError && err.code === 'plate_required' && err.plates) {
      const plates = err.plates
      const chosen = await askPlate(plates)
      if (chosen > 0) {
        await send(file, chosen)
      }
      return
    }
    if (err instanceof ApiError && err.status === 429) {
      ElMessage.warning(t('geometry.uploadBusy'))
      return
    }
    ElMessage.error(t('geometry.uploadFailed', { msg: err instanceof Error ? err.message : String(err) }))
  }
}

async function askPlate(plates: number[]): Promise<number> {
  try {
    const { value } = await ElMessageBox.prompt(t('geometry.choosePlate', { plates: plates.join(', ') }), t('geometry.plateTitle'), {
      inputValue: String(plates[0]),
      inputValidator: (v) => plates.includes(Number(v)) || t('geometry.plateInvalid'),
    })
    return Number(value)
  } catch {
    return 0 // cancelled
  }
}
</script>

<template>
  <div class="geometry-notice">
    <el-alert v-if="alert" :type="alert.type" :title="alert.text" :closable="false" show-icon />
    <el-button :loading="uploading" size="small" @click="input?.click()">
      <fa-icon icon="upload" class="ic" />{{ t('geometry.upload') }}
    </el-button>
    <input ref="input" type="file" accept=".gcode,.3mf" hidden @change="onFile" />
  </div>
</template>

<style scoped>
.geometry-notice {
  display: flex;
  align-items: center;
  gap: 12px;
}

.geometry-notice .el-alert {
  flex: 1;
}

.geometry-notice .el-button {
  margin-left: auto;
}
</style>
