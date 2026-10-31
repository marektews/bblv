<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PrinterInfo } from '../api'
import PrinterTile from '../components/PrinterTile.vue'
import { STATE_ICONS, STATE_ORDER, STATE_TYPES, stateKey } from '../format'
import { groupByState, loaded, printers } from '../store'

const { t, locale } = useI18n()

// Printers sorted by name and, when groupByState is on, grouped by state in
// STATE_ORDER; otherwise a single group.
const groups = computed(() => {
  const collator = new Intl.Collator(locale.value, { numeric: true, sensitivity: 'base' })
  if (!groupByState.value) {
    return [{ key: 'all', printers: [...printers.value].sort((a, b) => collator.compare(a.name, b.name)) }]
  }
  const byState = new Map<string, PrinterInfo[]>()
  for (const p of printers.value) {
    const key = stateKey(p)
    const list = byState.get(key)
    if (list) {
      list.push(p)
    } else {
      byState.set(key, [p])
    }
  }
  return STATE_ORDER.filter((key) => byState.has(key)).map((key) => ({
    key,
    printers: byState.get(key)!.sort((a, b) => collator.compare(a.name, b.name)),
  }))
})
</script>

<template>
  <div v-if="!loaded" v-loading="true" class="loading" />
  <el-empty v-else-if="printers.length === 0" :description="t('dashboard.empty')" />
  <section v-for="g in groups" :key="g.key" class="group">
    <h2 v-if="groupByState" :class="['group-title', `is-${STATE_TYPES[g.key] ?? 'info'}`]">
      <fa-icon :icon="STATE_ICONS[g.key]" class="ic" />{{ t(`state.${g.key}`) }}
      <span class="count">{{ g.printers.length }}</span>
    </h2>
    <div class="tiles">
      <PrinterTile v-for="p in g.printers" :key="p.id" :printer="p" />
    </div>
  </section>
</template>

<style scoped>
.loading {
  height: 200px;
}

.group + .group {
  margin-top: 24px;
}

.group-title {
  display: flex;
  align-items: center;
  margin: 0 0 10px;
  font-size: 15px;
  font-weight: 600;
}

.group-title .ic {
  color: var(--el-color-info);
}

.group-title.is-primary .ic {
  color: var(--el-color-primary);
}

.group-title.is-success .ic {
  color: var(--el-color-success);
}

.group-title.is-warning .ic {
  color: var(--el-color-warning);
}

.group-title.is-danger .ic {
  color: var(--el-color-danger);
}

.count {
  margin-left: 8px;
  padding: 0 7px;
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  background: var(--el-bg-color);
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: normal;
  line-height: 18px;
}

/* Wide enough for the name, model and tags in one row; never wider than the screen. */
.tiles {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 380px), 1fr));
}
</style>
