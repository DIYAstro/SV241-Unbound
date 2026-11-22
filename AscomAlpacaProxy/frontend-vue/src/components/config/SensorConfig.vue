<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import HelpTip from '../HelpTip.vue'

const store = useDeviceStore()
const modal = useModalStore()
const { config } = storeToRefs(store)

const sensorConfig = ref({})
const autoDryConfig = ref({})
const hasChanges = ref(false)
const autoDryHasChanges = ref(false)

// --- Lens probes (DS18B20) ---
// Rows: one per probe that is either found on the bus or assigned in the firmware config.
// { id, found, t (raw temp or null), heater (-1/0/1), offset, wasAssigned }
const probeRows = ref([])
const probesChanged = ref(false)
const scanning = ref(false)
const offsetSuggested = ref(false)
let lastFound = []          // last probe list reported by the firmware
let probeTimer = null
let probeRequestRunning = false
// The old single-probe offset ("so.dt"): the firmware no longer applies it, but it is offered as a
// starting value when a probe is assigned for the first time.
const legacyOffset = computed(() => Number(config.value?.so?.dt) || 0)

function heaterLabel(h) {
    const name = config.value?.dh?.[h]?.n
    return name ? `Dew Heater ${h + 1} (${name})` : `Dew Heater ${h + 1}`
}

// A heater already used by another probe is disabled in this probe's dropdown.
function heaterTaken(row, h) {
    return probeRows.value.some(r => r.id !== row.id && r.heater === h)
}

function mergeProbes() {
    const byId = new Map(probeRows.value.map(r => [r.id, r]))
    const out = []
    for (const p of lastFound) {
        let r = byId.get(p.id)
        if (!r) r = { id: p.id, heater: -1, offset: 0, wasAssigned: false }
        r.found = true
        r.t = p.t
        out.push(r)
        byId.delete(p.id)
    }
    for (const r of byId.values()) {
        r.found = false
        r.t = null
        out.push(r)
    }
    probeRows.value = out
}

function rebuildProbeRowsFromConfig(lp) {
    probeRows.value = (lp || []).map(e => ({
        id: e.id, heater: e.h, offset: e.o ?? 0, wasAssigned: e.h >= 0, found: false, t: null
    }))
    mergeProbes()
}

async function sendProbeCommand(cmd) {
    const response = await fetch('/api/v1/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(cmd)
    })
    if (!response.ok) throw new Error(response.statusText)
    const data = await response.json()
    if (data.error) throw new Error(data.error)
    return data
}

async function fetchProbes() {
    if (probeRequestRunning) return
    probeRequestRunning = true
    try {
        const data = await sendProbeCommand({ get: 'probes' })
        lastFound = data.probes || []
        mergeProbes()
    } catch (e) {
        // Device busy or disconnected - keep the last known list, try again on the next poll.
    } finally {
        probeRequestRunning = false
    }
}

async function rescanProbes() {
    scanning.value = true
    try {
        // Wait for a running poll first so the two requests don't overlap on the serial line.
        while (probeRequestRunning) await new Promise(r => setTimeout(r, 100))
        probeRequestRunning = true
        try {
            const data = await sendProbeCommand({ command: 'rescan_probes' })
            lastFound = data.probes || []
            mergeProbes()
        } finally {
            probeRequestRunning = false
        }
    } catch (e) {
        modal.error('Rescan failed: ' + e.message)
    } finally {
        scanning.value = false
    }
}

function onProbesChange() {
    probesChanged.value = true
}

// First assignment of a probe without an offset: offer the old single-probe offset.
function onHeaterChange(row) {
    if (row.heater >= 0 && !row.wasAssigned && !row.offset && legacyOffset.value) {
        row.offset = legacyOffset.value
        offsetSuggested.value = true
    }
    row.wasAssigned = row.heater >= 0
    onProbesChange()
}

function removeProbe(row) {
    probeRows.value = probeRows.value.filter(r => r.id !== row.id)
    onProbesChange()
}

async function saveProbes() {
    const lp = probeRows.value
        .filter(r => r.heater >= 0 || (parseFloat(r.offset) || 0) !== 0)
        .map(r => ({ id: r.id, o: parseFloat(r.offset) || 0, h: r.heater }))
    try {
        // store.saveConfig throws with the firmware's message if it rejects the assignment.
        await store.saveConfig({ lp })
        modal.success('Lens probe assignment saved.')
        probesChanged.value = false
        offsetSuggested.value = false
        rebuildProbeRowsFromConfig(config.value.lp)
    } catch (e) {
        modal.error('Error saving lens probes: ' + e.message)
    }
}

onMounted(() => {
    fetchProbes()
    probeTimer = setInterval(fetchProbes, 2000)
})

onUnmounted(() => {
    if (probeTimer) clearInterval(probeTimer)
})

// Rebuild the probe table when the firmware config changes - but never while the user is editing.
watch(() => config.value?.lp, (lp) => {
    if (!probesChanged.value) rebuildProbeRowsFromConfig(lp)
}, { immediate: true, deep: true })

watch(() => config.value, (newConfig) => {
    if (newConfig && !hasChanges.value) {
        // Sensor Offsets (so)
        // struct: st (SHT Temp), sh (SHT Hum), dt (legacy, no longer applied - see the Lens Probes
        // card), iv (INA Volt), ic (INA Curr)
        // Averaging counts ("ac") and update intervals ("ui") used to live here too, but were
        // removed - no legitimate reason to run them off their fixed firmware defaults, and
        // getting either wrong silently delays every reading (dew heater PID/current-limit
        // included). See sensors.cpp's SENSOR_MEDIAN_WINDOW/_INTERVAL_MS constants.
        const so = newConfig.so || {};

        sensorConfig.value = {
            // Offsets
            so_st: so.st ?? 0,
            so_sh: so.sh ?? 0,
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
                  <label>Temp Offset (°C)<HelpTip text="Added directly to the raw reading. If the sensor reads too high compared to a trusted reference thermometer, use a negative value." /></label>
                  <input type="number" v-model.number="sensorConfig.so_st" step="0.1" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Humidity Offset (%)<HelpTip text="Added directly to the raw reading. If the sensor reads too high compared to a trusted reference hygrometer, use a negative value." /></label>
                  <input type="number" v-model.number="sensorConfig.so_sh" step="0.1" @input="onChange">
              </div>
          </div>
      </div>

      <!-- Lens Probes (DS18B20) - up to two probes on the shared 1-Wire bus, each assigned to one
           dew heater by its unique ROM id. -->
      <div class="glass-panel settings-card">
          <h4>Lens Probes (DS18B20)</h4>
          <p class="subtitle">
              Each dew heater in PID or Minimum Temperature mode regulates on its own probe. A heater
              without an assigned (and found) probe stays off.
          </p>

          <table class="probe-table" v-if="probeRows.length">
              <thead>
                  <tr><th>Probe ID</th><th>Temp (°C)</th><th>Heater</th><th>Offset (°C)</th><th></th></tr>
              </thead>
              <tbody>
                  <tr v-for="row in probeRows" :key="row.id" :class="{ missing: !row.found }">
                      <td class="mono">{{ row.id }}</td>
                      <td>
                          <span v-if="!row.found" class="missing-label">not found</span>
                          <span v-else>{{ row.t != null ? row.t.toFixed(1) : '--' }}</span>
                      </td>
                      <td>
                          <select v-model.number="row.heater" @change="onHeaterChange(row)">
                              <option :value="-1">none</option>
                              <option v-for="h in [0, 1]" :key="h" :value="h" :disabled="heaterTaken(row, h)">
                                  {{ heaterLabel(h) }}
                              </option>
                          </select>
                      </td>
                      <td>
                          <input type="number" v-model.number="row.offset" step="0.1" class="offset-input" @input="onProbesChange">
                      </td>
                      <td>
                          <button v-if="!row.found" class="btn-secondary" @click="removeProbe(row)" title="Remove this assignment">Remove</button>
                      </td>
                  </tr>
              </tbody>
          </table>
          <p v-else class="subtitle">No probe found on the 1-Wire bus.</p>

          <small v-if="offsetSuggested" class="hint">
              The offset was pre-filled with the old single-probe offset ({{ legacyOffset }} °C) - check it and
              adjust if needed.
          </small>

          <div class="button-row">
              <button @click="saveProbes" class="btn-primary" :disabled="!probesChanged">Save Lens Probes</button>
              <button @click="rescanProbes" class="btn-secondary" :disabled="scanning">{{ scanning ? 'Scanning...' : 'Rescan' }}</button>
          </div>

          <small class="hint">
              Which probe is which? Warm one probe between your fingers and watch its live temperature
              rise, then assign it to the heater of that lens. Offset is added to the raw reading of
              that probe (negative if it reads too high against a trusted thermometer). A second probe
              connects to the Lens port with a Y-cable; click Rescan after plugging it in.
          </small>
      </div>

      <!-- INA219 (Power) -->
      <div class="glass-panel settings-card">
          <h4>INA219 (Power)</h4>
          <div class="card-grid">
              <div class="form-group">
                  <label>Voltage Offset (V)<HelpTip text="Added directly to the raw voltage reading. If the sensor reads too high compared to a trusted multimeter, use a negative value." /></label>
                  <input type="number" v-model.number="sensorConfig.so_iv" step="0.01" @input="onChange">
              </div>
              <div class="form-group">
                  <label>Current Offset (mA)<HelpTip text="Added directly to the raw current reading. If the sensor reads too high compared to a trusted clamp meter, use a negative value." /></label>
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
                  <HelpTip text="Automatically pulses the sensor's internal heater (~1s) to clear condensation off the SHT40 itself, once humidity has stayed above the threshold continuously for the Trigger Duration below." />
              </label>
          </div>

          <div class="card-grid">
              <div class="form-group">
                  <label>Humidity Threshold (%)<HelpTip text="Auto-Drying triggers once measured humidity reaches this level." /></label>
                  <input type="number" v-model.number="autoDryConfig.ht" min="0" max="100" @input="onAutoDryChange">
              </div>
              <div class="form-group">
                  <label>Trigger Duration (s)<HelpTip text="How long humidity must stay continuously above the threshold before the first drying cycle fires. If it's still above the threshold afterward, the next cycle fires right away instead of waiting again." /></label>
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

.probe-table {
    width: 100%;
    border-collapse: collapse;
    margin: 0.5rem 0;
}

.probe-table th,
.probe-table td {
    text-align: left;
    padding: 0.4rem 0.5rem;
    font-size: 0.85rem;
}

.probe-table tr.missing td {
    color: var(--danger-color, #e04040);
}

.mono {
    font-family: monospace;
}

.offset-input {
    width: 6rem;
}

.hint {
    display: block;
    margin-top: 0.5rem;
    color: var(--text-muted);
}
</style>
