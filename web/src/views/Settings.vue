<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  addPrinter,
  deletePrinter,
  getConfig,
  getDiscovered,
  importCodes,
  setPassword,
  updatePrinter,
  type AppConfig,
  type DiscoveredPrinter,
  type PrinterConfig,
  type PrinterInput,
} from '../api'
import PrinterDialog from '../components/PrinterDialog.vue'
import { auth, printers } from '../store'

const { t, locale } = useI18n()
// Connection state from the event stream; a printer is missing until the server reports it.
const onlineById = computed(() => new Map(printers.value.map((p) => [p.id, p.online])))

// Names and addresses sort naturally (A1#2 before A1#10, 192.168.4.40 before
// 192.168.4.212); serial numbers character by character.
const natural = computed(() => new Intl.Collator(locale.value, { numeric: true, sensitivity: 'base' }))
const byName = (a: PrinterConfig, b: PrinterConfig) => natural.value.compare(a.name, b.name)
const byHost = (a: PrinterConfig, b: PrinterConfig) => natural.value.compare(a.host, b.host)
const bySerial = (a: PrinterConfig, b: PrinterConfig) => a.serial.localeCompare(b.serial)

// The table's last sort order, remembered in the browser.
type SortOrder = 'ascending' | 'descending'
const SORT_KEY = 'printerSort'
function savedSort(): { prop: string; order: SortOrder } | undefined {
  try {
    const s = JSON.parse(localStorage.getItem(SORT_KEY) ?? 'null')
    if (['name', 'host', 'serial'].includes(s?.prop) && ['ascending', 'descending'].includes(s?.order)) {
      return { prop: s.prop, order: s.order }
    }
  } catch {
    // Unavailable storage or a damaged entry; the table starts unsorted.
  }
  return undefined
}
const printerSort = savedSort()

function onSortChange({ prop, order }: { prop: string | null; order: SortOrder | null }) {
  try {
    if (prop && order) {
      localStorage.setItem(SORT_KEY, JSON.stringify({ prop, order }))
    } else {
      localStorage.removeItem(SORT_KEY)
    }
  } catch {
    // Not remembered; the table starts unsorted next time.
  }
}
const config = ref<AppConfig | null>(null)
const discovered = ref<DiscoveredPrinter[]>([])
const discoveryError = ref('')
const dialog = ref<Partial<PrinterConfig> | null>(null)
const editingId = ref('')
const saving = ref(false)
const importing = ref(false)
const confFile = ref<HTMLInputElement>()
const pw = reactive({ current: '', password: '', repeat: '', busy: false })

const message = (err: unknown) => (err instanceof Error ? err.message : String(err))

async function load() {
  try {
    config.value = await getConfig()
  } catch (err) {
    ElMessage.error(message(err))
  }
}

async function loadDiscovered() {
  try {
    const d = await getDiscovered()
    discovered.value = d.devices
    discoveryError.value = d.error ?? ''
  } catch {
    // Shown again on the next refresh.
  }
}

// Printers announce themselves about every 10 seconds.
let timer = 0
onMounted(() => {
  load()
  loadDiscovered()
  timer = window.setInterval(loadDiscovered, 5000)
})
onBeforeUnmount(() => clearInterval(timer))

function openNew(from?: DiscoveredPrinter) {
  editingId.value = ''
  dialog.value = from ? { name: from.name, model: from.model, host: from.host, serial: from.serial } : {}
}

function openEdit(p: PrinterConfig) {
  editingId.value = p.id
  dialog.value = p
}

async function save(input: PrinterInput) {
  saving.value = true
  try {
    if (editingId.value) {
      await updatePrinter(editingId.value, input)
    } else {
      await addPrinter(input)
    }
    dialog.value = null
    ElMessage.success(t('settings.saved'))
    await Promise.all([load(), loadDiscovered()])
  } catch (err) {
    ElMessage.error(message(err))
  } finally {
    saving.value = false
  }
}

async function remove(p: PrinterConfig) {
  try {
    await ElMessageBox.confirm(t('settings.confirmDelete', { name: p.name }), { type: 'warning' })
  } catch {
    return // cancelled
  }
  try {
    await deletePrinter(p.id)
    await Promise.all([load(), loadDiscovered()])
  } catch (err) {
    ElMessage.error(message(err))
  }
}

async function runImport(file?: File) {
  importing.value = true
  try {
    const r = await importCodes(file)
    ElMessage.success(t('settings.importDone', { updated: r.updated.length, found: r.found }))
    if (r.missing.length) {
      ElMessage.warning(t('settings.importMissing', { ids: r.missing.join(', ') }))
    }
    await load()
  } catch (err) {
    ElMessage.error(message(err))
  } finally {
    importing.value = false
  }
}

function onConfFile(e: Event) {
  const el = e.target as HTMLInputElement
  const file = el.files?.[0]
  el.value = ''
  if (file) {
    runImport(file)
  }
}

const pwMismatch = computed(() => pw.password !== pw.repeat)

async function changePassword(remove = false) {
  pw.busy = true
  try {
    await setPassword(pw.current, remove ? '' : pw.password)
    Object.assign(pw, { current: '', password: '', repeat: '' })
    auth.required = !remove
    ElMessage.success(remove ? t('settings.passwordRemoved') : t('settings.passwordSaved'))
    await load()
  } catch (err) {
    ElMessage.error(message(err))
  } finally {
    pw.busy = false
  }
}
</script>

<template>
  <div class="settings">
    <h2><fa-icon icon="gear" class="ic" />{{ t('settings.title') }}</h2>

    <section>
      <div class="section-head">
        <h3><fa-icon icon="print" class="ic" />{{ t('settings.printers') }}</h3>
        <el-button size="small" @click="openNew()"><fa-icon icon="plus" class="ic" />{{ t('settings.addManually') }}</el-button>
      </div>
      <el-table
        :data="config?.printers ?? []"
        :empty-text="t('settings.noPrinters')"
        :default-sort="printerSort"
        size="small"
        @sort-change="onSortChange"
      >
        <el-table-column prop="name" :label="t('settings.name')" min-width="140" sortable :sort-method="byName" />
        <el-table-column prop="model" :label="t('settings.model')" width="100" />
        <el-table-column prop="host" :label="t('settings.host')" width="140" sortable :sort-method="byHost" />
        <el-table-column prop="serial" :label="t('settings.serial')" min-width="150" sortable :sort-method="bySerial" />
        <el-table-column :label="t('settings.accessCode')" width="130">
          <template #default="{ row }">
            <el-tag :type="row.hasAccessCode ? 'success' : 'danger'" size="small">
              <fa-icon :icon="row.hasAccessCode ? 'key' : 'xmark'" class="ic" />
              {{ row.hasAccessCode ? t('settings.codeSet') : t('settings.codeMissing') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('settings.connection')" width="120">
          <template #default="{ row }">
            <el-tag v-if="onlineById.has(row.id)" :type="onlineById.get(row.id) ? 'success' : 'info'" size="small">
              <fa-icon :icon="onlineById.get(row.id) ? 'circle-check' : 'plug-circle-xmark'" class="ic" />
              {{ onlineById.get(row.id) ? t('settings.online') : t('state.OFFLINE') }}
            </el-tag>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column width="200" align="right">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row as PrinterConfig)"><fa-icon icon="pen" class="ic" />{{ t('settings.edit') }}</el-button>
            <el-button size="small" type="danger" plain @click="remove(row as PrinterConfig)">
              <fa-icon icon="trash" class="ic" />{{ t('settings.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <section>
      <h3><fa-icon icon="magnifying-glass" class="ic" />{{ t('settings.discovered') }}</h3>
      <p class="hint">{{ t('settings.discoveredHint') }}</p>
      <el-alert v-if="discoveryError" type="warning" :title="t('settings.discoveryUnavailable', { msg: discoveryError })" :closable="false" />
      <el-table :data="discovered" :empty-text="t('settings.noneDiscovered')" size="small">
        <el-table-column prop="name" :label="t('settings.name')" min-width="140" />
        <el-table-column prop="model" :label="t('settings.model')" width="100" />
        <el-table-column prop="host" :label="t('settings.host')" width="140" />
        <el-table-column prop="serial" :label="t('settings.serial')" min-width="150" />
        <el-table-column prop="firmware" :label="t('status.firmware')" width="120" />
        <el-table-column width="170" align="right">
          <template #default="{ row }">
            <el-tag v-if="row.configuredAs" size="small" type="info"><fa-icon icon="check" class="ic" />{{ t('settings.alreadyAdded') }}</el-tag>
            <el-button v-else size="small" type="primary" @click="openNew(row as DiscoveredPrinter)">
              <fa-icon icon="plus" class="ic" />{{ t('settings.add') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <section>
      <h3><fa-icon icon="file-import" class="ic" />{{ t('settings.importTitle') }}</h3>
      <p class="hint">{{ t('settings.importHint') }}</p>
      <p v-if="config" class="hint">
        <code>{{ config.studioConfig.path }}</code> —
        {{ config.studioConfig.found ? t('settings.studioFound') : t('settings.studioNotFound') }}
      </p>
      <div class="buttons">
        <el-button :disabled="!config?.studioConfig.found" :loading="importing" @click="runImport()">
          <fa-icon icon="desktop" class="ic" />{{ t('settings.importLocal') }}
        </el-button>
        <el-button :loading="importing" @click="confFile?.click()"><fa-icon icon="file-arrow-up" class="ic" />{{ t('settings.importFile') }}</el-button>
        <input ref="confFile" type="file" accept=".conf" hidden @change="onConfFile" />
      </div>
    </section>

    <section>
      <h3><fa-icon icon="lock" class="ic" />{{ t('settings.password') }}</h3>
      <p class="hint">{{ config?.passwordSet ? t('settings.passwordSetHint') : t('settings.passwordHint') }}</p>
      <el-form label-position="top" class="password" @submit.prevent="changePassword()">
        <el-form-item v-if="config?.passwordSet" :label="t('settings.currentPassword')">
          <el-input v-model="pw.current" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-form-item :label="t('settings.newPassword')">
          <el-input v-model="pw.password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item :label="t('settings.repeatPassword')" :error="pw.repeat && pwMismatch ? t('settings.passwordMismatch') : ''">
          <el-input v-model="pw.repeat" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <div class="buttons">
          <el-button type="primary" native-type="submit" :loading="pw.busy" :disabled="!pw.password || pwMismatch">
            <fa-icon icon="floppy-disk" class="ic" />{{ config?.passwordSet ? t('settings.changePassword') : t('settings.setPassword') }}
          </el-button>
          <el-button v-if="config?.passwordSet" :loading="pw.busy" :disabled="!pw.current" @click="changePassword(true)">
            <fa-icon icon="lock-open" class="ic" />{{ t('settings.removePassword') }}
          </el-button>
        </div>
      </el-form>
    </section>

    <PrinterDialog :initial="dialog" :editing="!!editingId" :saving="saving" @save="save" @close="dialog = null" />
  </div>
</template>

<style scoped>
.settings {
  max-width: 1100px;
  margin: 0 auto;
}

section {
  margin-bottom: 28px;
}

h2 {
  margin: 0 0 16px;
  font-size: 20px;
}

h3 {
  margin: 0 0 8px;
  font-size: 15px;
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.section-head h3 {
  margin: 0;
}

.hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.buttons {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.password {
  max-width: 360px;
}

.el-alert {
  margin-bottom: 8px;
}
</style>
