<template>
  <span class="plugin-owned-tag" :title="t('settings.plugins.managedByHint')">
    {{ t('settings.plugins.managedBy', { name }) }}
  </span>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ name: string }>()
const { t } = useI18n()
</script>

<style scoped lang="less">
.plugin-owned-tag {
  flex-shrink: 0;
  max-width: 45%;
  padding: 0 6px;
  overflow: hidden;
  border-radius: var(--app-radius-pill);
  background: color-mix(in srgb, var(--td-brand-color) 8%, transparent);
  color: var(--td-brand-color);
  font-size: var(--app-text-xs);
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
