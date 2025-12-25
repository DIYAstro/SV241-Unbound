<script setup>
import { ref, onMounted } from 'vue'
import SwitchConfig from './config/SwitchConfig.vue'
import HeaterConfig from './config/HeaterConfig.vue'
import SensorConfig from './config/SensorConfig.vue'
import SystemSettings from './config/SystemSettings.vue'
import ProxySettings from './config/ProxySettings.vue'

const activeTab = ref('tab-switches')

const tabs = [
    { id: 'tab-switches', label: 'Switches', component: SwitchConfig },
    { id: 'tab-heaters', label: 'Dew Heaters', component: HeaterConfig },
    { id: 'tab-sensors', label: 'Sensors/Auto-Dry', component: SensorConfig },
    { id: 'tab-system', label: 'System', component: SystemSettings },
    { id: 'tab-proxy', label: 'Proxy', component: ProxySettings },
]

const isCollapsed = ref(true) // Default to collapsed

onMounted(() => {
    const savedState = localStorage.getItem('collapsed-configuration')
    if (savedState === 'false') {
        isCollapsed.value = false
    }
})

function toggleCollapse() {
    isCollapsed.value = !isCollapsed.value
    localStorage.setItem('collapsed-configuration', isCollapsed.value)
}
</script>

<template>
  <div id="config-section" class="glass-panel card full-width">
      <div class="collapsible-header" @click="toggleCollapse">
          <h2>Configuration & Settings</h2>
          <span class="toggle-icon" :class="{ collapsed: isCollapsed }"></span>
      </div>

      <div class="collapsible-content" :class="{ collapsed: isCollapsed }">
          <div class="tabs">
              <button 
                  v-for="tab in tabs" 
                  :key="tab.id"
                  class="tab-btn" 
                  :class="{ active: activeTab === tab.id }"
                  @click="activeTab = tab.id"
              >
                  {{ tab.label }}
              </button>
          </div>

          <div class="tab-content active">
             <component :is="tabs.find(t => t.id === activeTab)?.component || 'div'" />
             <div v-if="!tabs.find(t => t.id === activeTab)?.component">
                 <p style="padding: 1rem; text-align: center; color: var(--text-muted);">
                     Settings for {{ tabs.find(t => t.id === activeTab)?.label }} coming soon.
                 </p>
             </div>
          </div>
      </div>
  </div>
</template>

<style scoped>
</style>
