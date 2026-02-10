<script setup>
import { useDeviceStore } from '../stores/device'
import { useThemeStore } from '../stores/theme'
import { storeToRefs } from 'pinia'

const store = useDeviceStore()
const themeStore = useThemeStore()
const { firmwareVersion, comPort, connectionStatus, isConnected, proxyVersion } = storeToRefs(store)
const { currentTheme } = storeToRefs(themeStore)
const { THEMES } = themeStore

const handleThemeChange = (event) => {
    themeStore.setTheme(event.target.value)
}
</script>

<template>
  <header class="main-header glass-panel">
      <div class="header-content">
          <h1>SV241 Unbound</h1>
          <span class="subtitle">Alpaca Driver & Controller <span id="proxy-version-display">{{ proxyVersion }}</span></span>
      </div>
      <div class="header-badges">
          <div class="theme-selector">
              <label for="theme-select" class="theme-label">
                  <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path>
                  </svg>
              </label>
              <select id="theme-select" v-model="currentTheme" @change="handleThemeChange" class="theme-select">
                  <option :value="THEMES.DEFAULT">Default</option>
                  <option :value="THEMES.DARK">Dark</option>
                  <option :value="THEMES.RED">Red</option>
              </select>
          </div>
          <div class="status-badge" id="com-port-badge">
              <span class="value">{{ comPort }}</span>
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

<style scoped>
.theme-selector {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: var(--surface-color);
    padding: 0.5rem 0.75rem;
    border-radius: 50px;
    border: 1px solid var(--surface-border);
    transition: all 0.3s ease;
}

.theme-selector:hover {
    background: var(--surface-hover);
    border-color: rgba(255, 255, 255, 0.2);
}

.theme-selector:focus-within {
    border-color: var(--primary-color);
    box-shadow: 0 0 0 2px rgba(0, 210, 255, 0.2);
    background: rgba(255, 255, 255, 0.1);
}

.theme-label {
    display: flex;
    align-items: center;
    margin: 0;
    color: var(--text-secondary);
    cursor: pointer;
}

.theme-label svg {
    transition: transform 0.3s ease;
}

.theme-selector:hover .theme-label svg {
    transform: rotate(20deg);
}

.theme-select {
    background: rgba(255, 255, 255, 0.01); /* Nearly invisible, for Firefox color-scheme support */
    border: none;
    color: var(--text-primary);
    font-size: 0.9rem;
    cursor: pointer;
    outline: none;
    padding: 0;
    font-family: inherit;
    font-weight: 500;
    color-scheme: dark;
    box-shadow: none !important; /* Disable individual focus shadow */
}
</style>
