<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, watch } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { proxyConfig, liveStatus, isConnected } = storeToRefs(store)

// Local edit buffer, separate from the store's live value - same pattern as ProxySettings.vue's
// localConfig/hasChanges. proxyConfig gets reassigned to a brand-new object on every 2s settings
// poll (device.js's checkConnection) regardless of whether anything actually changed, so without
// the hasChanges guard the very next poll tick overwrites whatever the user just typed.
const threshold = ref(0)
const uiWarningEnabled = ref(false)
const alpacaEnabled = ref(false)
const hasChanges = ref(false)
watch(proxyConfig, (val) => {
    if (!val || hasChanges.value) return
    threshold.value = val.safetyMonitorVoltageThreshold ?? 0
    uiWarningEnabled.value = val.safetyMonitorUIWarningEnabled ?? false
    alpacaEnabled.value = val.safetyMonitorAlpacaEnabled ?? false
}, { immediate: true })

function onChange() {
    hasChanges.value = true
}

async function save() {
    try {
        await store.saveProxyConfig({
            ...store.proxyConfig,
            safetyMonitorVoltageThreshold: parseFloat(threshold.value) || 0,
            safetyMonitorUIWarningEnabled: uiWarningEnabled.value,
            safetyMonitorAlpacaEnabled: alpacaEnabled.value
        })
        hasChanges.value = false
        modal.success('Safety Monitor settings saved.')
    } catch (e) {
        modal.error('Error saving Safety Monitor settings: ' + e.message)
    }
}
</script>

<template>
  <div class="config-group full-width-group">
      <h3>Safety Monitor</h3>

      <div class="settings-card glass-panel">
          <h4>Status</h4>
          <p class="card-description">
              Current input voltage vs. the configured safety threshold below.
          </p>
          <div class="status-row">
              <span class="status-value">
                  {{ isConnected ? (liveStatus.v || 0).toFixed(2) : '--' }} V
              </span>
              <span v-if="isConnected" class="status-badge" :class="liveStatus.unsafe ? 'unsafe' : 'safe'">
                  {{ liveStatus.unsafe ? 'UNSAFE' : 'SAFE' }}
              </span>
          </div>
      </div>

      <div class="settings-card glass-panel">
          <h4>Threshold</h4>
          <p class="card-description">
              If input voltage drops to or below this value, the box is considered "unsafe" - useful
              for a battery-powered rig in the field, so a sequencer (e.g. N.I.N.A.) can shut down in
              an orderly way before the battery is fully drained.
          </p>
          <div class="card-grid">
              <div class="form-group">
                  <label>Safety Voltage Threshold (V)</label>
                  <input type="number" step="0.1" min="0" v-model.number="threshold" @input="onChange" placeholder="0 = disabled">
              </div>
          </div>
      </div>

      <div class="settings-card glass-panel">
          <h4>Notification Channels</h4>
          <p class="card-description">
              Choose independently where a threshold crossing gets reported.
          </p>
          <div class="checkbox-with-hint">
              <label class="checkbox-label">
                  <input type="checkbox" v-model="uiWarningEnabled" @change="onChange">
                  Show warning indicator & desktop notification
              </label>
              <small class="hint">Adds a warning dot in Live Telemetry and sends a desktop notification when the state changes.</small>
          </div>
          <div class="checkbox-with-hint">
              <label class="checkbox-label">
                  <input type="checkbox" v-model="alpacaEnabled" @change="onChange">
                  Expose ASCOM Alpaca SafetyMonitor device
              </label>
              <small class="hint">Adds a SafetyMonitor device at /api/v1/safetymonitor/0/ that ASCOM client software (e.g. N.I.N.A.) can poll to abort a sequence.</small>
          </div>
      </div>

      <button @click="save" class="btn-primary full-width-btn" :disabled="!hasChanges">Save Safety Monitor Settings</button>
  </div>
</template>

<style scoped>
.settings-card {
    padding: 1.25rem;
    margin-bottom: 1rem;
}

.settings-card h4 {
    margin: 0 0 0.5rem 0;
    color: var(--primary-color);
    font-size: 1rem;
    font-weight: 600;
}

.card-description {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin: 0 0 0.75rem 0;
}

.card-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
}

.form-group {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
}

.form-group label {
    font-size: 0.85rem;
    color: var(--text-secondary, #aaa);
}

.checkbox-with-hint {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    margin-bottom: 0.75rem;
}

.checkbox-with-hint:last-child {
    margin-bottom: 0;
}

.checkbox-label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    color: var(--text-secondary, #aaa);
    font-size: 0.9rem;
}

.hint {
    font-size: 0.8rem;
    color: var(--text-muted, #666);
    display: block;
}

.status-row {
    display: flex;
    align-items: center;
    gap: 1rem;
}

.status-value {
    font-size: 1.25rem;
    font-weight: 600;
    color: var(--text-color);
}

.status-badge {
    padding: 0.25rem 0.75rem;
    border-radius: 6px;
    font-size: 0.8rem;
    font-weight: 700;
    letter-spacing: 0.5px;
}

.status-badge.safe {
    background: rgba(64, 200, 64, 0.15);
    color: #40c840;
}

.status-badge.unsafe {
    background: rgba(224, 64, 64, 0.15);
    color: var(--danger-color, #e04040);
}

.full-width-btn {
    margin-top: 0.5rem;
    width: 100%;
}

@media (max-width: 600px) {
    .card-grid {
        grid-template-columns: 1fr;
    }
}
</style>
