<script setup lang="ts">
import { computed, defineAsyncComponent, defineComponent, h } from 'vue'
import { useI18n } from 'vue-i18n'
import CameraPanel from '../components/CameraPanel.vue'
import FilamentPanel from '../components/FilamentPanel.vue'
import GeometryNotice from '../components/GeometryNotice.vue'
import StateTag from '../components/StateTag.vue'
import StatusPanel from '../components/StatusPanel.vue'
import { reloadForUpdate } from '../reload'
import { loaded, printers } from '../store'

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
    <el-alert v-if="!printer.online" type="warning" :closable="false" show-icon class="offline-note" :title="t('printer.offline')" :description="printer.connectionError" />
    <div class="layout" :class="{ offline: !printer.online }">
      <section class="main">
        <GeometryNotice :printer="printer" />
        <PrintViewer :key="printer.id" :printer="printer" />
        <FilamentPanel :printer="printer" />
      </section>
      <aside class="side">
        <CameraPanel v-if="printer.camera" :key="printer.id" :printer="printer" />
        <StatusPanel :printer="printer" />
      </aside>
    </div>
  </div>
  <el-empty v-else-if="loaded" :description="t('printer.notFound')" />
  <div v-else v-loading="true" class="loading" />
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

.offline-note {
  margin-bottom: 12px;
}

/* The last report of an offline printer: AMS, alerts and the device are
   no longer current. Sections, as on a phone their panel has no box. */
.offline :deep(.status-panel > section),
.offline :deep(.filament-panel) {
  opacity: 0.55;
}

.viewer-failed {
  padding: 24px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  text-align: center;
}

.loading {
  height: 200px;
}

/* On a narrow screen the columns dissolve into one list: the progress and
   the camera first, then the 3D view and the rest of the status. */
@media (max-width: 900px) {
  .layout {
    display: flex;
    flex-direction: column;
    align-items: stretch; /* the 3D view has no width of its own */
  }

  .main,
  .side,
  .side :deep(.status-panel) {
    display: contents;
  }

  .side :deep(.status-panel > section) {
    order: 4;
  }

  .side :deep(.status-panel > section.progress) {
    order: 1;
  }

  .side :deep(.camera) {
    order: 2;
  }

  .main > :deep(*) {
    order: 3;
  }
}
</style>
