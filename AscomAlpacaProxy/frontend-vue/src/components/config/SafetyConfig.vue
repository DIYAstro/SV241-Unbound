<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, watch, computed } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { proxyConfig, liveStatus, isConnected } = storeToRefs(store)

// Every metric the proxy currently caches per live sensor poll (serial.Conditions.Data) and
// therefore can evaluate a condition against - see internal/serial's safetyMetricExtractors,
// which this list must stay in sync with.
const METRICS = [
    { id: 'voltage', label: 'Voltage', unit: 'V' },
    { id: 'current', label: 'Current', unit: 'A' },
    { id: 'power', label: 'Power', unit: 'W' },
    { id: 'ambientTemp', label: 'Ambient Temperature', unit: '°C' },
    { id: 'humidity', label: 'Humidity', unit: '%' },
    { id: 'dewPoint', label: 'Dew Point', unit: '°C' },
    { id: 'lensTemp', label: 'Lens Temperature', unit: '°C' },
]
const OPERATORS = [
    { id: '>', label: '>' },
    { id: '>=', label: '≥' },
    { id: '<', label: '<' },
    { id: '<=', label: '≤' },
]

function metricInfo(id) {
    return METRICS.find(m => m.id === id) || { label: id, unit: '' }
}
function operatorLabel(id) {
    return OPERATORS.find(o => o.id === id)?.label || id
}

// Mirrors internal/serial's safetyMetricExtractors - must stay in sync with that map. Needed
// client-side so the Conditions list can show each row's live value and highlight which
// condition(s) are actually the ones currently tripped, rather than only the two aggregate
// SAFE/UNSAFE states the backend exposes via liveStatus.unsafeUI/unsafeAlpaca.
function extractMetricValue(metric, status) {
    switch (metric) {
        case 'voltage': return status.v
        case 'current': return typeof status.i === 'number' ? status.i / 1000 : undefined
        case 'power': return status.p
        case 'ambientTemp': return status.t_amb
        case 'humidity': return status.h_amb
        case 'dewPoint': return status.d
        case 'lensTemp': return status.t_lens
        default: return undefined
    }
}

function evaluateCondition(operator, value, threshold) {
    if (typeof value !== 'number') return false
    switch (operator) {
        case '>': return value > threshold
        case '>=': return value >= threshold
        case '<': return value < threshold
        case '<=': return value <= threshold
        default: return false
    }
}

function formatValue(value, unit) {
    return typeof value === 'number' ? `${value.toFixed(1)} ${unit}` : '--'
}

// Each saved condition, enriched with its current live value and whether it's the one (or one of
// several) currently tripping "unsafe" - drives both the per-row highlight below and the Status
// card's list of active violations.
const conditionStatuses = computed(() => conditions.value.map(cond => {
    const value = extractMetricValue(cond.metric, liveStatus.value || {})
    return {
        ...cond,
        currentValue: value,
        triggered: isConnected.value && evaluateCondition(cond.operator, value, cond.threshold)
    }
}))
const triggeredConditions = computed(() => conditionStatuses.value.filter(c => c.triggered))

// Local edit buffer, separate from the store's live value - same pattern as ProxySettings.vue's
// localConfig/hasChanges. proxyConfig gets reassigned to a brand-new object on every 2s settings
// poll (device.js's checkConnection) regardless of whether anything actually changed, so without
// the hasChanges guard the very next poll tick overwrites whatever the user just typed/added/removed.
const conditions = ref([])
const notifyConnectionEvents = ref(true)
const notifyHeaterCurrentLimit = ref(true)
const hasChanges = ref(false)
watch(proxyConfig, (val) => {
    if (!val || hasChanges.value) return
    conditions.value = JSON.parse(JSON.stringify(val.safetyMonitorConditions || []))
    notifyConnectionEvents.value = val.notifyConnectionEvents ?? true
    notifyHeaterCurrentLimit.value = val.notifyHeaterCurrentLimit ?? true
}, { immediate: true })

function onChange() {
    hasChanges.value = true
}

// Draft fields for the "add condition" row. Notify defaults on, IncludeInSafetyMonitor defaults
// off - same convention the backend used to apply when migrating an old single threshold.
const newMetric = ref(METRICS[0].id)
const newOperator = ref(OPERATORS[0].id)
const newThreshold = ref(0)
const newNotify = ref(true)
const newIncludeInSafetyMonitor = ref(false)

function addCondition() {
    conditions.value.push({
        metric: newMetric.value,
        operator: newOperator.value,
        threshold: parseFloat(newThreshold.value) || 0,
        notify: newNotify.value,
        includeInSafetyMonitor: newIncludeInSafetyMonitor.value
    })
    newThreshold.value = 0
    onChange()
}

function removeCondition(index) {
    conditions.value.splice(index, 1)
    onChange()
}

async function save() {
    try {
        await store.saveProxyConfig({
            ...store.proxyConfig,
            safetyMonitorConditions: conditions.value,
            notifyConnectionEvents: notifyConnectionEvents.value,
            notifyHeaterCurrentLimit: notifyHeaterCurrentLimit.value
        })
        hasChanges.value = false
        modal.success('Safety Monitor & Notifications settings saved.')
    } catch (e) {
        modal.error('Error saving settings: ' + e.message)
    }
}
</script>

<template>
  <div class="config-group full-width-group">
      <h3>Safety Monitor & Notifications</h3>

      <div class="settings-card glass-panel">
          <h4>General Notifications</h4>
          <p class="card-description">
              Every proxy event that can send a desktop notification, each independently switchable.
          </p>
          <div class="checkbox-with-hint">
              <label class="checkbox-label">
                  <input type="checkbox" v-model="notifyConnectionEvents" @change="onChange">
                  Notify on Connection Lost/Restored
              </label>
              <small class="hint">Desktop notification when the SV241's serial connection drops or comes back.</small>
          </div>
          <div class="checkbox-with-hint">
              <label class="checkbox-label">
                  <input type="checkbox" v-model="notifyHeaterCurrentLimit" @change="onChange">
                  Notify on Heater Current-Limit Change
              </label>
              <small class="hint">Desktop notification when dew heater output starts or stops being reduced by the box-wide current limit.</small>
          </div>
      </div>

      <div class="settings-card glass-panel">
          <h4>Status</h4>
          <p class="card-description">
              Current state vs. the conditions configured below, per channel.
          </p>
          <div class="status-row" v-if="isConnected">
              <span class="status-badge" :class="liveStatus.unsafeUI ? 'unsafe' : 'safe'">
                  Notify: {{ liveStatus.unsafeUI ? 'UNSAFE' : 'SAFE' }}
              </span>
              <span class="status-badge" :class="liveStatus.unsafeAlpaca ? 'unsafe' : 'safe'">
                  Safety Monitor: {{ liveStatus.unsafeAlpaca ? 'UNSAFE' : 'SAFE' }}
              </span>
          </div>
          <span v-else class="status-value">--</span>
          <ul v-if="isConnected && triggeredConditions.length" class="triggered-list">
              <li v-for="(cond, idx) in triggeredConditions" :key="idx">
                  {{ metricInfo(cond.metric).label }} is {{ formatValue(cond.currentValue, metricInfo(cond.metric).unit) }}
                  (condition: {{ operatorLabel(cond.operator) }} {{ cond.threshold }} {{ metricInfo(cond.metric).unit }})
                  <span v-if="cond.notify" class="channel-tag">Notify</span>
                  <span v-if="cond.includeInSafetyMonitor" class="channel-tag">Safety Monitor</span>
              </li>
          </ul>
      </div>

      <div class="settings-card glass-panel">
          <h4>Conditions</h4>
          <p class="card-description">
              Each condition independently decides whether it fires a desktop notification
              ("Notify") and/or counts toward the ASCOM SafetyMonitor device's IsSafe ("Include in
              Safety Monitor") - e.g. "humidity too high" can be notification-only while "voltage
              critically low" also aborts a N.I.N.A. sequence.
          </p>

          <div v-if="conditions.length" class="condition-list">
              <div v-for="(cond, idx) in conditions" :key="idx" class="condition-row" :class="{ triggered: conditionStatuses[idx]?.triggered }">
                  <span class="condition-text">
                      {{ metricInfo(cond.metric).label }} {{ operatorLabel(cond.operator) }} {{ cond.threshold }} {{ metricInfo(cond.metric).unit }}
                      <small v-if="isConnected" class="condition-current">(currently {{ formatValue(conditionStatuses[idx]?.currentValue, metricInfo(cond.metric).unit) }})</small>
                  </span>
                  <div class="condition-channels">
                      <label><input type="checkbox" v-model="cond.notify" @change="onChange"> Notify</label>
                      <label><input type="checkbox" v-model="cond.includeInSafetyMonitor" @change="onChange"> Include in Safety Monitor</label>
                  </div>
                  <button @click="removeCondition(idx)" class="btn-danger">Remove</button>
              </div>
          </div>
          <p v-else class="card-description">No conditions configured - IsSafe always reports true.</p>

          <div class="add-condition-row">
              <select v-model="newMetric">
                  <option v-for="m in METRICS" :key="m.id" :value="m.id">{{ m.label }}</option>
              </select>
              <select v-model="newOperator">
                  <option v-for="o in OPERATORS" :key="o.id" :value="o.id">{{ o.label }}</option>
              </select>
              <input type="number" step="0.1" v-model.number="newThreshold" :placeholder="metricInfo(newMetric).unit">
              <label><input type="checkbox" v-model="newNotify"> Notify</label>
              <label><input type="checkbox" v-model="newIncludeInSafetyMonitor"> Include in Safety Monitor</label>
              <button @click="addCondition" class="btn-secondary">Add</button>
          </div>
      </div>

      <button @click="save" class="btn-primary full-width-btn" :disabled="!hasChanges">Save Settings</button>
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

.condition-list {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    margin-bottom: 0.75rem;
    max-height: 260px;
    overflow-y: auto;
}

.condition-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.5rem 0.6rem;
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.04);
    font-size: 0.85rem;
    border: 1px solid transparent;
}

.condition-row.triggered {
    background: rgba(224, 64, 64, 0.1);
    border-color: rgba(224, 64, 64, 0.4);
}

.condition-text {
    font-family: monospace;
    color: var(--text-color);
    /* Grows to absorb the leftover space so .condition-channels/the Remove button always land at
       the same fixed spot from the right edge, regardless of how long this row's metric name +
       value text is - without this, justify-content:space-between split the row's own leftover
       space into gaps whose size varied per row, so the checkboxes visibly zig-zagged between
       rows. */
    flex: 1 1 auto;
    min-width: 0;
}

.condition-current {
    font-family: inherit;
    color: var(--text-muted, #888);
    margin-left: 0.35rem;
}

.triggered-list {
    margin: 0.75rem 0 0 0;
    padding-left: 1.25rem;
    font-size: 0.85rem;
    color: var(--danger-color, #e04040);
}

.triggered-list li {
    margin-bottom: 0.25rem;
}

.condition-row button {
    flex: none;
}

.condition-channels {
    display: flex;
    flex: none;
    gap: 0.75rem;
    flex-wrap: wrap;
    font-size: 0.8rem;
    color: var(--text-secondary, #aaa);
}

.condition-channels label {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    cursor: pointer;
    white-space: nowrap;
}

.channel-tag {
    display: inline-block;
    margin-left: 0.5rem;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: rgba(224, 64, 64, 0.15);
    font-size: 0.75rem;
    font-weight: 600;
}

.add-condition-row {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    align-items: center;
}

.add-condition-row > select,
.add-condition-row > input {
    flex: 1;
    min-width: 100px;
}

.add-condition-row > label {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    flex: none;
    white-space: nowrap;
    font-size: 0.85rem;
    color: var(--text-secondary, #aaa);
    cursor: pointer;
}

.add-condition-row button {
    flex: none;
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
