<script setup>
import { computed } from 'vue'
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'

const store = useDeviceStore()
const { firmwareVersion, comPort, connectionStatus, isConnected, proxyVersion, activeRigName, liveStatus } = storeToRefs(store)

// Seconds since the SV241's controller booted ("up" in the sensors response). If it ever drops back
// to a small number while the proxy has been running for much longer, the controller restarted.
// Older firmware doesn't report it, in which case the badge stays hidden.
const uptimeText = computed(() => {
    const seconds = liveStatus.value?.up
    if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) return ''
    const pad = (n) => String(n).padStart(2, '0')
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return `${pad(days)}:${pad(hours)}:${pad(minutes)}`
})

const RESET_REASONS = {
    1: 'power-on', 3: 'software reset', 4: 'firmware crash', 5: 'watchdog', 6: 'watchdog', 7: 'watchdog', 9: 'brownout'
}
const uptimeTitle = computed(() => {
    const reason = liveStatus.value?.rr
    const text = RESET_REASONS[reason] ?? (typeof reason === 'number' ? `other (${reason})` : null)
    return text ? `Controller uptime (dd:hh:mm) - last start: ${text}` : 'Controller uptime (dd:hh:mm)'
})
</script>

<template>
  <header class="main-header glass-panel">
      <div class="header-content">
          <h1>SV241 Unbound</h1>
          <span class="subtitle">Alpaca Driver & Controller <span id="proxy-version-display">{{ proxyVersion }}</span></span>
      </div>
      <div class="header-badges">
          <div class="status-badge" id="com-port-badge">
              <span class="value">{{ comPort }}</span>
          </div>
          <div class="status-badge" id="rig-name-badge" v-if="activeRigName">
              <span class="label">Rig</span>
              <span class="value">{{ activeRigName }}</span>
          </div>
          <div class="status-badge" id="firmware-badge">
              <span class="label">FW</span>
              <span class="value">{{ firmwareVersion }}</span>
          </div>
          <div class="status-badge" id="uptime-badge" v-if="uptimeText" :title="uptimeTitle">
              <span class="label">Up</span>
              <span class="value">{{ uptimeText }}</span>
          </div>
          <div class="connection-pill" id="connection-status-pill">
              <span id="connection-indicator" :class="{ connected: isConnected, disconnected: !isConnected }"></span>
              <span id="connection-text">{{ connectionStatus }}</span>
          </div>
      </div>
  </header>
</template>
