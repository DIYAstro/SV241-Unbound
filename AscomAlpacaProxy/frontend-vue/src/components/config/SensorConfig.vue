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

watch(() => config.value, (newConfig) => {
    if (newConfig && !hasChanges.value) {
        // Sensor Offsets (so)
        // struct: st (SHT Temp), sh (SHT Hum), dt (DS Temp), iv (INA Volt), ic (INA Curr)
        const so = newConfig.so || {};
        const ac = newConfig.ac || {};
        const ui = newConfig.ui || {};

        sensorConfig.value = {
            // Offsets
            so_st: so.st ?? 0,
            so_sh: so.sh ?? 0,
            so_dt: so.dt ?? 0,
            so_iv: so.iv ?? 0,
            so_ic: so.ic ?? 0,

            // Averaging Counts
            ac_st: ac.st ?? 10,
            ac_sh: ac.sh ?? 10,
            ac_dt: ac.dt ?? 10,
            ac_iv: ac.iv ?? 10,
            ac_ic: ac.ic ?? 10,

            // Update Intervals
            ui_s: ui.s ?? 1000, // SHT40
            ui_d: ui.d ?? 1000, // DS18B20
            ui_i: ui.i ?? 1000, // INA219
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

async function saveSensors() {
    const payload = {
        so: {
            st: parseFloat(sensorConfig.value.so_st),
            sh: parseFloat(sensorConfig.value.so_sh),
            dt: parseFloat(sensorConfig.value.so_dt),
            iv: parseFloat(sensorConfig.value.so_iv),
            ic: parseFloat(sensorConfig.value.so_ic)
        },
        ac: {
            st: parseInt(sensorConfig.value.ac_st),
            sh: parseInt(sensorConfig.value.ac_sh),
            dt: parseInt(sensorConfig.value.ac_dt),
            iv: parseInt(sensorConfig.value.ac_iv),
            ic: parseInt(sensorConfig.value.ac_ic)
        },
        ui: {
            s: parseInt(sensorConfig.value.ui_s),
            d: parseInt(sensorConfig.value.ui_d),
            i: parseInt(sensorConfig.value.ui_i)
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
  <div class="config-group full-width-group sensor-settings">
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
              <div class="form-group">
                  <label>Averaging</label>
                  <input type="number" v-model.number="sensorConfig.ac_st" min="1" max="50" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Interval (ms)</label>
                  <input type="number" v-model.number="sensorConfig.ui_s" min="100" step="100" @input="onChange">
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
              <div class="form-group">
                  <label>Averaging</label>
                  <input type="number" v-model.number="sensorConfig.ac_dt" min="1" max="50" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Interval (ms)</label>
                  <input type="number" v-model.number="sensorConfig.ui_d" min="100" step="100" @input="onChange">
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
              <div class="form-group">
                  <label>Averaging (V)</label>
                  <input type="number" v-model.number="sensorConfig.ac_iv" min="1" max="50" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Averaging (I)</label>
                  <input type="number" v-model.number="sensorConfig.ac_ic" min="1" max="50" @input="onChange">
              </div>
              <div class="form-group full-width">
                  <label>Interval (ms)</label>
                  <input type="number" v-model.number="sensorConfig.ui_i" min="100" step="100" @input="onChange">
              </div>
          </div>
      </div>
      
      <button @click="saveSensors" class="btn-primary full-width-btn" :disabled="!hasChanges">Save Sensor Settings</button>

      <!-- Auto Drying -->
      <div class="config-group full-width-group">
          <h3>Auto-Drying</h3>
          <p class="subtitle">Automatically heat the sensor if humidity is high to prevent saturation.</p>
          
          <div class="form-group">
              <label>
                  <input type="checkbox" v-model="autoDryConfig.en">
                  Enable Auto-Drying
              </label>
          </div>
          
          <div class="form-grid">
              <div class="form-group">
                  <label>Humidity Threshold (%)</label>
                  <input type="number" v-model.number="autoDryConfig.ht" min="0" max="100">
              </div>
              <div class="form-group">
                  <label>Trigger Duration (s)</label>
                  <input type="number" v-model.number="autoDryConfig.td" min="0" max="600">
              </div>
          </div>

          <div class="button-row">
              <button @click="saveAutoDry" class="btn-primary">Save Auto-Dry Settings</button>
              <button @click="triggerDry" class="btn-secondary">Trigger Manual Dry Cycle</button>
          </div>
      </div>
  </div>
</template>

<style scoped>
.sensor-settings {
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

.form-group {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
}

.form-group label {
    font-size: 0.85rem;
    color: var(--text-secondary, #aaa);
}

.form-group.full-width {
    grid-column: span 2;
}

.full-width-btn {
    margin-top: 1rem;
    width: 100%;
}

.form-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
    margin-bottom: 1rem;
}

.button-row {
    display: flex;
    gap: 1rem;
    margin-top: 1rem;
}

@media (max-width: 600px) {
    .card-grid {
        grid-template-columns: 1fr;
    }
    .form-group.full-width {
        grid-column: span 1;
    }
}
</style>
