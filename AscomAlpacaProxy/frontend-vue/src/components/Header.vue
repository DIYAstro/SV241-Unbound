<script setup>
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'

const store = useDeviceStore()
const { firmwareVersion, comPort, connectionStatus, isConnected, proxyVersion, activeRigName } = storeToRefs(store)
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
          <div class="connection-pill" id="connection-status-pill">
              <span id="connection-indicator" :class="{ connected: isConnected, disconnected: !isConnected }"></span>
              <span id="connection-text">{{ connectionStatus }}</span>
          </div>
      </div>
  </header>
</template>
