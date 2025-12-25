<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, watch, computed } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { proxyConfig } = storeToRefs(store)

const localConfig = ref({})
const hasChanges = ref(false)

// Master Power Name is stored in switchNames['master_power']
const masterPowerName = computed({
    get: () => localConfig.value?.switchNames?.['master_power'] || '',
    set: (val) => {
        if (!localConfig.value.switchNames) localConfig.value.switchNames = {};
        localConfig.value.switchNames['master_power'] = val;
    }
});

watch(() => proxyConfig.value, (newVal) => {
    if (newVal && !hasChanges.value) {
        localConfig.value = JSON.parse(JSON.stringify(newVal));
    }
}, { immediate: true, deep: true })

function onChange() {
    hasChanges.value = true;
}

async function save() {
    // Ensure numeric types
    localConfig.value.networkPort = parseInt(localConfig.value.networkPort);
    localConfig.value.historyRetentionNights = parseInt(localConfig.value.historyRetentionNights);

    try {
        await store.saveProxyConfig(localConfig.value);
        modal.success('Proxy settings saved. Some changes may require an application restart.', 'Settings Saved');
        hasChanges.value = false;
    } catch (e) {
        modal.error('Error saving: ' + e.message);
    }
}
</script>

<template>
  <div class="config-group full-width-group proxy-settings">
      <h3>Proxy Settings</h3>
      
      <!-- Connection Settings Card -->
      <div class="settings-card glass-panel">
          <h4>Connection Settings</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Serial Port</label>
                  <input type="text" v-model="localConfig.serialPortName" @input="onChange" 
                         :disabled="localConfig.autoDetectPort"
                         :placeholder="localConfig.autoDetectPort ? 'Auto-detecting...' : 'e.g. COM3'">
              </div>
              <div class="form-group checkbox-group">
                  <label>
                      <input type="checkbox" v-model="localConfig.autoDetectPort" @change="onChange">
                      Auto-Detect Port
                  </label>
              </div>
              <div class="form-group">
                  <label>Listen Address</label>
                  <input type="text" v-model="localConfig.listenAddress" @input="onChange" placeholder="127.0.0.1">
              </div>
              <div class="form-group">
                  <label>Network Port</label>
                  <input type="number" v-model.number="localConfig.networkPort" @input="onChange" placeholder="32241">
              </div>
          </div>
      </div>

      <!-- Logging & Telemetry Card -->
      <div class="settings-card glass-panel">
          <h4>Logging & Telemetry</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Log Level</label>
                  <select v-model="localConfig.logLevel" @change="onChange">
                      <option value="DEBUG">DEBUG</option>
                      <option value="INFO">INFO</option>
                      <option value="WARN">WARN</option>
                      <option value="ERROR">ERROR</option>
                  </select>
              </div>
              <div class="form-group">
                   <label>Telemetry Interval</label>
                   <select v-model.number="localConfig.telemetryInterval" @change="onChange">
                       <option :value="0">Disabled</option>
                       <option :value="1">1 second</option>
                       <option :value="2">2 seconds</option>
                       <option :value="3">3 seconds</option>
                       <option :value="5">5 seconds</option>
                       <option :value="10">10 seconds</option>
                   </select>
              </div>
              <div class="form-group full-width">
                   <label>Min. Telemetry Retention (Nights)</label>
                   <input type="number" v-model.number="localConfig.historyRetentionNights" @input="onChange" min="0">
                   <small class="hint">Keeps at least this many recorded nights. Set to 0 for unlimited.</small>
              </div>
          </div>
      </div>

      <!-- ASCOM/Alpaca Features Card -->
      <div class="settings-card glass-panel">
          <h4>ASCOM/Alpaca Features</h4>
          <div class="card-content">
              <div class="checkbox-with-hint">
                   <label class="checkbox-label">
                       <input type="checkbox" v-model="localConfig.enableAlpacaVoltageControl" @change="onChange">
                       Enable Alpaca Voltage Control
                   </label>
                   <small class="hint">Allow setting adjustable voltage via ASCOM Switch interface.</small>
              </div>
              
              <hr class="divider">
              
              <div class="master-power-section">
                  <div class="master-power-row">
                      <div class="form-group">
                          <label>Master Power Switch</label>
                          <select v-model="localConfig.enableMasterPower" @change="onChange">
                              <option :value="true">Enabled</option>
                              <option :value="false">Disabled</option>
                          </select>
                      </div>
                      <div class="form-group">
                          <label>Custom Name</label>
                          <input type="text" 
                                 v-model="masterPowerName" 
                                 @input="onChange"
                                 :disabled="!localConfig.enableMasterPower"
                                 placeholder="Master Power">
                      </div>
                  </div>
                  <small class="hint">Virtual switch to control all outputs simultaneously.</small>
              </div>
          </div>
      </div>
      
      <button @click="save" class="btn-primary save-btn" :disabled="!hasChanges">Save Proxy Settings</button>
  </div>
</template>

<style scoped>
.proxy-settings {
    display: flex;
    flex-direction: column;
    gap: 1rem;
}

.settings-card {
    padding: 1.25rem;
}

.settings-card h4 {
    margin: 0 0 1rem 0;
    color: var(--primary-color);
    font-size: 1rem;
    font-weight: 600;
}

.card-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
}

.card-content {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
}

.master-power-row {
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

.checkbox-group {
    flex-direction: row;
    align-items: flex-end;
    justify-content: flex-start;
    padding-bottom: 0.5rem;
}

.checkbox-group label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
}

.checkbox-with-hint {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
}

.checkbox-label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    color: var(--text-secondary, #aaa);
    font-size: 0.85rem;
}

.master-power-section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
}

.full-width {
    grid-column: span 2;
}

.hint {
    font-size: 0.8rem;
    color: var(--text-muted, #666);
    display: block;
}

.divider {
    border: none;
    border-top: 1px solid rgba(255, 255, 255, 0.1);
    margin: 0.5rem 0;
}

.save-btn {
    align-self: flex-start;
    margin-top: 0.5rem;
}

@media (max-width: 600px) {
    .card-grid, .master-power-row {
        grid-template-columns: 1fr;
    }
    .full-width {
        grid-column: span 1;
    }
}
</style>

