<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PrinterTile from '../components/PrinterTile.vue'
import { connected, printers } from '../store'

const { t } = useI18n()
</script>

<template>
  <el-empty v-if="connected && printers.length === 0" :description="t('dashboard.empty')" />
  <div class="tiles">
    <PrinterTile v-for="p in printers" :key="p.id" :printer="p" />
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
}
</style>
