<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, watch } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { config } = storeToRefs(store)

const sensorConfig = ref({})
const autoDryConfig = ref({})
const hasChanges = ref(false)
const autoDryHasChanges = ref(false)

watch(() => config.value, (newConfig) => {
    if (newConfig && !hasChanges.value) {
        // Sensor Offsets (so)
        // struct: st (SHT Temp), sh (SHT Hum), dt (DS Temp), iv (INA Volt), ic (INA Curr)
        // Averaging counts ("ac") and update intervals ("ui") used to live here too, but were
        // removed - no legitimate reason to run them off their fixed firmware defaults, and
        // getting either wrong silently delays every reading (dew heater PID/current-limit
        // included). See sensors.cpp's SENSOR_MEDIAN_WINDOW/_INTERVAL_MS constants.
        const so = newConfig.so || {};

        sensorConfig.value = {
            // Offsets
            so_st: so.st ?? 0,
            so_sh: so.sh ?? 0,
            so_dt: so.dt ?? 0,
            so_iv: so.iv ?? 0,
            so_ic: so.ic ?? 0,
        };

        // Auto dry: Config.ad
        if (newConfig.ad) {
            autoDryConfig.value = { 
                ...newConfig.ad,
                en: !!newConfig.ad.en  // Convert 1 -> true, 0 -> false
            };
        } else {
            autoDryConfig.value = { en: false, ht: 75, td: 10 };
        }
    }
}, { immediate: true, deep: true })

function onChange() {
    hasChanges.value = true;
}

function onAutoDryChange() {
    autoDryHasChanges.value = true;
}

async function saveSensors() {
    const payload = {
        so: {
            st: parseFloat(sensorConfig.value.so_st),
            sh: parseFloat(sensorConfig.value.so_sh),
            dt: parseFloat(sensorConfig.value.so_dt),
            iv: parseFloat(sensorConfig.value.so_iv),
            ic: parseFloat(sensorConfig.value.so_ic)
        }
    };

    try {
        await store.saveConfig(payload);
        modal.success('Sensor settings saved.');
        hasChanges.value = false;
    } catch (e) {
        modal.error('Error saving: ' + e.message);
    }
}

async function saveAutoDry() {
    const payload = {
        ad: {
            en: autoDryConfig.value.en ? 1 : 0,  // Convert boolean to 0/1 for firmware
            ht: parseInt(autoDryConfig.value.ht),
            td: parseInt(autoDryConfig.value.td)
        }
    };
    try {
        await store.saveConfig(payload);
        modal.success('Auto-drying settings saved.');
        autoDryHasChanges.value = false;
    } catch (e) {
        modal.error('Error saving: ' + e.message);
    }
}

async function triggerDry() {
    modal.confirm('Activate sensor heater temporarily? This will affect readings.', {
        title: 'Trigger Sensor Drying',
        confirmText: 'Activate',
        cancelText: 'Cancel',
        onConfirm: async () => {
            try {
                await fetch('/api/v1/command', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ command: 'dry_sensor' })
                });
                modal.success('Sensor drying triggered.');
            } catch (e) {
                modal.error('Error: ' + e.message);
            }
        }
    });
}

</script>

<template>
  <div class="config-group">
      <h3>Sensor Calibration & Configuration</h3>
      
      <!-- SHT40 (Ambient) -->
      <div class="glass-panel settings-card">
          <h4>SHT40 (Ambient)</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Temp Offset (°C)</label>
                  <input type="number" v-model.number="sensorConfig.so_st" step="0.1" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Humidity Offset (%)</label>
                  <input type="number" v-model.number="sensorConfig.so_sh" step="0.1" @input="onChange">
              </div>
          </div>
      </div>

      <!-- DS18B20 (Lens) -->
      <div class="glass-panel settings-card">
          <h4>DS18B20 (Lens)</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Temp Offset (°C)</label>
                  <input type="number" v-model.number="sensorConfig.so_dt" step="0.1" @input="onChange">
              </div>
          </div>
      </div>

      <!-- INA219 (Power) -->
      <div class="glass-panel settings-card">
          <h4>INA219 (Power)</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Voltage Offset (V)</label>
                  <input type="number" v-model.number="sensorConfig.so_iv" step="0.01" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Current Offset (mA)</label>
                  <input type="number" v-model.number="sensorConfig.so_ic" step="0.01" @input="onChange">
              </div>
          </div>
      </div>
      
      <button @click="saveSensors" class="btn-primary full-width-btn" :disabled="!hasChanges">Save Sensor Settings</button>

      <!-- Auto Drying -->
      <div class="config-group">
          <h3>Auto-Drying</h3>
          <p class="subtitle">Automatically heat the sensor if humidity is high to prevent saturation.</p>

          <div class="form-group">
              <label class="checkbox-label">
                  <input type="checkbox" v-model="autoDryConfig.en" @change="onAutoDryChange">
                  Enable Auto-Drying
              </label>
          </div>

          <div class="card-grid">
              <div class="form-group">
                  <label>Humidity Threshold (%)</label>
                  <input type="number" v-model.number="autoDryConfig.ht" min="0" max="100" @input="onAutoDryChange">
              </div>
              <div class="form-group">
                  <label>Trigger Duration (s)</label>
                  <input type="number" v-model.number="autoDryConfig.td" min="0" max="600" @input="onAutoDryChange">
              </div>
          </div>

          <div class="button-row">
              <button @click="saveAutoDry" class="btn-primary" :disabled="!autoDryHasChanges">Save Auto-Dry Settings</button>
              <button @click="triggerDry" class="btn-secondary">Trigger Manual Dry Cycle</button>
          </div>
      </div>
  </div>
</template>

<style scoped>
.form-group {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
}

.form-group label {
    font-size: 0.85rem;
    color: var(--text-secondary);
}

.button-row {
    display: flex;
    gap: 1rem;
    margin-top: 1rem;
}
</style>
