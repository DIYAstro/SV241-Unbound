<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { useThemeStore } from '../../stores/theme'
import { storeToRefs } from 'pinia'
import { ref, watch, computed } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { proxyConfig, availableIps } = storeToRefs(store)

// Theme is a client-side/browser preference (localStorage-backed, applied instantly) -
// deliberately kept separate from localConfig/hasChanges/save() below, which is for the
// server-persisted ProxyConfig fields. It shouldn't need a "Save" click to take effect, same as
// it never did in its previous home (the header).
const themeStore = useThemeStore()
const { currentTheme } = storeToRefs(themeStore)
const { THEMES } = themeStore
function handleThemeChange(event) {
    themeStore.setTheme(event.target.value)
}

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
  <div class="config-group proxy-settings">
      <h3>Proxy Settings</h3>

      <!-- Appearance Card - client-side only, applies instantly, no Save button needed -->
      <div class="settings-card glass-panel">
          <h4>Appearance</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Theme</label>
                  <select :value="currentTheme" @change="handleThemeChange">
                      <option :value="THEMES.DARK">Material Dark</option>
                      <option :value="THEMES.DEFAULT">Deep Space</option>
                      <option :value="THEMES.RED">Night Vision</option>
                  </select>
              </div>
          </div>
      </div>

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
              <div class="form-group checkbox-row">
                  <label class="checkbox-label">
                      <input type="checkbox" v-model="localConfig.autoDetectPort" @change="onChange">
                      Auto-Detect Port
                  </label>
              </div>
              <div class="form-group">
                  <label>Listen Address</label>
                  <select v-model="localConfig.listenAddress" @change="onChange">
                      <option v-for="ip in availableIps" :key="ip" :value="ip">{{ ip }}</option>
                  </select>
              </div>
              <div class="card-grid">
                  <div class="form-group">
                      <label>Network Port</label>
                      <input type="number" v-model.number="localConfig.networkPort" @input="onChange" placeholder="32241">
                  </div>
                  <div class="form-group">
                      <label>Discovery Service</label>
                      <select v-model="localConfig.enableAlpacaDiscovery" @change="onChange">
                          <option :value="true">Enabled</option>
                          <option :value="false">Disabled</option>
                      </select>
                  </div>
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
                       Enable Variable Voltage Control (Alpaca & WebUI)
                   </label>
                   <small class="hint">Allow setting adjustable voltage via ASCOM Switch interface.</small>
              </div>
              
              <hr class="divider">
              
               <div class="master-power-section">
                  <div class="card-grid">
                      <div class="checkbox-with-hint">
                           <label class="checkbox-label">
                               <input type="checkbox" v-model="localConfig.alwaysShowLensTemp" @change="onChange">
                               Always expose 'Lens Temperature'
                           </label>
                           <small class="hint">Always show sensor reading, even if PID/MinTemp is disabled.</small>
                      </div>
                      <div class="form-group">
                          <label>Custom Lens Temperature Sensor Name</label>
                          <input type="text" 
                                 v-model="localConfig.lensTempName" 
                                 @input="onChange"
                                 placeholder="Lens Temperature">
                      </div>
                  </div>
              </div>

               <hr class="divider">
               
               <div class="master-power-section">
                  <div class="card-grid">
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
      
      <button @click="save" class="btn-primary full-width-btn" :disabled="!hasChanges">Save Proxy Settings</button>
  </div>
</template>

<style scoped>
.proxy-settings {
    display: flex;
    flex-direction: column;
    gap: 1rem;
}

.card-content {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
}

.form-group {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
}

.form-group label {
    font-size: 0.85rem;
    color: var(--text-secondary);
}

/* .checkbox-label (global) already handles the flex/gap/color/cursor look for the checkbox
   itself - this only aligns the row alongside its sibling label+input fields in the same
   .card-grid (which sit below their own label, so a bare checkbox needs align-items:flex-end to
   match their baseline). */
.checkbox-row {
    flex-direction: row;
    align-items: flex-end;
    justify-content: flex-start;
    gap: 1.5rem;
    padding-bottom: 0.5rem;
}

.master-power-section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
}

.divider {
    border: none;
    border-top: 1px solid rgba(255, 255, 255, 0.1);
    margin: 0.5rem 0;
}
</style>

