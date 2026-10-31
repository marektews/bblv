<script setup lang="ts">
import { computed, defineAsyncComponent, defineComponent, h } from 'vue'
import { useI18n } from 'vue-i18n'
import CameraPanel from '../components/CameraPanel.vue'
import GeometryNotice from '../components/GeometryNotice.vue'
import StateTag from '../components/StateTag.vue'
import StatusPanel from '../components/StatusPanel.vue'
import { reloadForUpdate } from '../reload'
import { connected, printers } from '../store'

// Shown when the 3D view cannot be loaded and reloading did not help.
const ViewerFailed = defineComponent({
  props: { error: { type: Error, default: undefined } },
  setup(p) {
    const i18n = useI18n()
    return () => h('div', { class: 'viewer-failed' }, i18n.t('viewer.loadFailed', { msg: p.error?.message ?? '' }))
  },
})

// Three.js is loaded only when a printer is opened. A page loaded before the
// server was updated asks for a file of the old build, which is gone; it
// reloads to get the new one.
const PrintViewer = defineAsyncComponent({
  loader: () => import('../components/PrintViewer.vue'),
  errorComponent: ViewerFailed,
  onError(_err, _retry, fail) {
    if (!reloadForUpdate()) {
      fail()
    }
  },
})

const props = defineProps<{ id: string }>()
const { t } = useI18n()
const printer = computed(() => printers.value.find((p) => p.id === props.id))
</script>

<template>
  <div v-if="printer" class="printer-view">
    <div class="head">
      <router-link to="/" class="back"><fa-icon icon="arrow-left" class="ic" />{{ t('printer.back') }}</router-link>
      <h2>{{ printer.name }}</h2>
      <span class="model">{{ printer.model }}</span>
      <StateTag :printer="printer" />
      <el-tooltip v-if="printer.experimental" :content="t('printer.experimentalHint')">
        <el-tag type="warning" size="small"><fa-icon icon="flask" class="ic" />{{ t('printer.experimental') }}</el-tag>
      </el-tooltip>
    </div>
    <div class="layout">
      <section class="main">
        <GeometryNotice :printer="printer" />
        <PrintViewer :key="printer.id" :printer="printer" />
      </section>
      <aside class="side">
        <CameraPanel v-if="printer.camera" :key="printer.id" :printer="printer" />
        <StatusPanel :printer="printer" />
      </aside>
    </div>
  </div>
  <el-empty v-else-if="connected" :description="t('printer.notFound')" />
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.head h2 {
  margin: 0;
  font-size: 20px;
}

.back,
.model {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.back {
  flex-basis: 100%;
}

.layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  gap: 16px;
  align-items: start;
}

.main,
.side {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.viewer-failed {
  padding: 24px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  text-align: center;
}

@media (max-width: 900px) {
  .layout {
    grid-template-columns: 1fr;
  }
}
</style>
