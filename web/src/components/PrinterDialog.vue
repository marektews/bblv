<script setup lang="ts">
import type { FormInstance, FormRules } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PrinterConfig, PrinterInput } from '../api'

// The names network discovery gives (internal/discovery).
const MODELS = ['A1', 'A1 mini', 'A2L', 'P1P', 'P1S', 'P2S', 'X1', 'X1C', 'X1E', 'X2D', 'H2D', 'H2D Pro', 'H2S', 'H2C']

const props = defineProps<{
  // The printer being edited, or the values to start a new one with.
  initial: Partial<PrinterConfig> | null
  editing: boolean
  saving: boolean
}>()
const emit = defineEmits<{ save: [input: PrinterInput]; close: [] }>()
const { t } = useI18n()

const form = ref<FormInstance>()
const values = reactive<PrinterInput>({ name: '', model: '', host: '', serial: '', accessCode: '' })
const visible = computed(() => props.initial !== null)

watch(
  () => props.initial,
  (p) => {
    Object.assign(values, { name: p?.name ?? '', model: p?.model ?? '', host: p?.host ?? '', serial: p?.serial ?? '', accessCode: '' })
    form.value?.clearValidate()
  },
)

// The server gives the saved access code only to the serial number it was
// entered for; another one, which any device could claim, needs it again.
const keepsCode = computed(
  () => props.editing && !!props.initial?.hasAccessCode && values.serial.trim().toUpperCase() === (props.initial?.serial ?? '').toUpperCase(),
)

const rules = computed<FormRules>(() => ({
  host: [{ required: true, message: t('settings.required'), trigger: 'blur' }],
  serial: [{ required: true, message: t('settings.required'), trigger: 'blur' }],
  accessCode: [
    {
      validator: (_rule, value: string, callback) =>
        props.editing && props.initial?.hasAccessCode && !keepsCode.value && !value.trim()
          ? callback(new Error(t('settings.accessCodeForSerial')))
          : callback(),
      trigger: 'blur',
    },
  ],
}))

async function submit() {
  if (await form.value?.validate().catch(() => false)) {
    emit('save', { ...values })
  }
}
</script>

<template>
  <el-dialog :model-value="visible" :title="editing ? t('settings.editPrinter') : t('settings.addPrinter')" width="min(520px, 95vw)" @close="emit('close')">
    <el-form ref="form" :model="values" :rules="rules" label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('settings.name')" prop="name">
        <el-input v-model="values.name" :placeholder="t('settings.namePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('settings.model')" prop="model">
        <el-select v-model="values.model" filterable allow-create default-first-option clearable>
          <el-option v-for="m in MODELS" :key="m" :label="m" :value="m" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('settings.host')" prop="host">
        <el-input v-model="values.host" placeholder="192.168.1.50" />
      </el-form-item>
      <el-form-item :label="t('settings.serial')" prop="serial">
        <el-input v-model="values.serial" />
      </el-form-item>
      <el-form-item :label="t('settings.accessCode')" prop="accessCode">
        <el-input
          v-model="values.accessCode"
          type="password"
          show-password
          autocomplete="off"
          :placeholder="keepsCode ? t('settings.keepAccessCode') : ''"
        />
        <div class="hint">{{ t('settings.accessCodeHint') }}</div>
      </el-form-item>
      <button type="submit" hidden />
    </el-form>
    <template #footer>
      <el-button @click="emit('close')">{{ t('settings.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="submit"><fa-icon icon="floppy-disk" class="ic" />{{ t('settings.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.el-select {
  width: 100%;
}

.hint {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--el-text-color-secondary);
}
</style>
