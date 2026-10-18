<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import GeometryNotice from '../components/GeometryNotice.vue'
import StateTag from '../components/StateTag.vue'
import StatusPanel from '../components/StatusPanel.vue'
import { connected, printers } from '../store'

// Three.js is loaded only when a printer is opened.
const PrintViewer = defineAsyncComponent(() => import('../components/PrintViewer.vue'))

const props = defineProps<{ id: string }>()
const { t } = useI18n()
const printer = computed(() => printers.value.find((p) => p.id === props.id))
</script>

<template>
  <div v-if="printer" class="printer-view">
    <div class="head">
      <router-link to="/" class="back">← {{ t('printer.back') }}</router-link>
      <h2>{{ printer.name }}</h2>
      <span class="model">{{ printer.model }}</span>
      <StateTag :printer="printer" />
      <el-tooltip v-if="printer.experimental" :content="t('printer.experimentalHint')">
        <el-tag type="warning" size="small">{{ t('printer.experimental') }}</el-tag>
      </el-tooltip>
    </div>
    <div class="layout">
      <section class="main">
        <GeometryNotice :printer="printer" />
        <PrintViewer :key="printer.id" :printer="printer" />
      </section>
      <aside>
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

.main {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

@media (max-width: 900px) {
  .layout {
    grid-template-columns: 1fr;
  }
}
</style>
