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
// which this list must stay in sync with. The two "event" entries don't have a live numeric
// value/threshold at all - they represent discrete, edge-triggered occurrences (see
// internal/serial's fireSafetyEvent) and use a second "event" dropdown instead of operator+value.
const METRICS = [
    { id: 'voltage', label: 'Voltage', unit: 'V', kind: 'numeric' },
    { id: 'current', label: 'Current', unit: 'A', kind: 'numeric' },
    { id: 'power', label: 'Power', unit: 'W', kind: 'numeric' },
    { id: 'ambientTemp', label: 'Ambient Temperature', unit: '°C', kind: 'numeric' },
    { id: 'humidity', label: 'Humidity', unit: '%', kind: 'numeric' },
    { id: 'dewPoint', label: 'Dew Point', unit: '°C', kind: 'numeric' },
    { id: 'lensTemp', label: 'Lens Temperature', unit: '°C', kind: 'numeric' },
    { id: 'connection', label: 'Connection', kind: 'event', events: [
        { id: 'connectionLost', label: 'Lost' },
        { id: 'connectionRestored', label: 'Restored' },
    ]},
    { id: 'heaterCurrentLimit', label: 'Heater Current-Limit', kind: 'event', events: [
        { id: 'heaterLimitEngaged', label: 'Engaged' },
        { id: 'heaterLimitCleared', label: 'Cleared' },
    ]},
]
const OPERATORS = [
    { id: '>', label: '>' },
    { id: '>=', label: '≥' },
    { id: '<', label: '<' },
    { id: '<=', label: '≤' },
]

// Flat label lookup for event-metric rows (they have no operator/threshold to render from).
const EVENT_METRIC_LABELS = {
    connectionLost: 'Connection: Lost',
    connectionRestored: 'Connection: Restored',
    heaterLimitEngaged: 'Heater Current-Limit: Engaged',
    heaterLimitCleared: 'Heater Current-Limit: Cleared',
}
function isEventMetric(metric) {
    return metric in EVENT_METRIC_LABELS
}

// Only the two "bad state" events can meaningfully be included in the ASCOM SafetyMonitor
// calculation - "restored"/"cleared" are momentary recoveries with nothing to include (see
// internal/serial's applyConnectionLostSafetyState/booleanSafetyMetrics for how these two are
// actually evaluated, which differs between them since polling stops entirely on disconnect).
const SAFETY_MONITOR_ELIGIBLE_EVENTS = ['connectionLost', 'heaterLimitEngaged']

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
const hasChanges = ref(false)
watch(proxyConfig, (val) => {
    if (!val || hasChanges.value) return
    conditions.value = JSON.parse(JSON.stringify(val.safetyMonitorConditions || []))
}, { immediate: true })

function onChange() {
    hasChanges.value = true
}

// Draft fields for the "add condition" row. Notify defaults on, IncludeInSafetyMonitor defaults
// off - same convention the backend used to apply when migrating an old single threshold.
const newCategory = ref(METRICS[0].id)
const selectedCategory = computed(() => METRICS.find(m => m.id === newCategory.value))
const newOperator = ref(OPERATORS[0].id)
const newThreshold = ref(0)
const newEventMetric = ref(null)
const newNotify = ref(true)
const newIncludeInSafetyMonitor = ref(false)

// Reset the event sub-selection whenever the category changes, so switching e.g. from
// "Connection" to "Heater Current-Limit" doesn't leave a stale connectionLost/Restored value
// selected underneath a now-unrelated category.
watch(newCategory, (id) => {
    const category = METRICS.find(m => m.id === id)
    newEventMetric.value = category?.kind === 'event' ? category.events[0].id : null
}, { immediate: true })

const showIncludeCheckbox = computed(() =>
    selectedCategory.value?.kind === 'numeric' || SAFETY_MONITOR_ELIGIBLE_EVENTS.includes(newEventMetric.value)
)

function addCondition() {
    const category = selectedCategory.value
    const metricId = category.kind === 'numeric' ? category.id : newEventMetric.value
    const eligible = category.kind === 'numeric' || SAFETY_MONITOR_ELIGIBLE_EVENTS.includes(metricId)
    conditions.value.push({
        metric: metricId,
        operator: category.kind === 'numeric' ? newOperator.value : '',
        threshold: category.kind === 'numeric' ? (parseFloat(newThreshold.value) || 0) : 0,
        notify: newNotify.value,
        includeInSafetyMonitor: eligible ? newIncludeInSafetyMonitor.value : false
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
            safetyMonitorConditions: conditions.value
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
                      <template v-if="isEventMetric(cond.metric)">
                          {{ EVENT_METRIC_LABELS[cond.metric] }}
                      </template>
                      <template v-else>
                          {{ metricInfo(cond.metric).label }} {{ operatorLabel(cond.operator) }} {{ cond.threshold }} {{ metricInfo(cond.metric).unit }}
                          <small v-if="isConnected" class="condition-current">(currently {{ formatValue(conditionStatuses[idx]?.currentValue, metricInfo(cond.metric).unit) }})</small>
                      </template>
                  </span>
                  <div class="condition-channels">
                      <label><input type="checkbox" v-model="cond.notify" @change="onChange"> Notify</label>
                      <label v-if="!isEventMetric(cond.metric) || SAFETY_MONITOR_ELIGIBLE_EVENTS.includes(cond.metric)">
                          <input type="checkbox" v-model="cond.includeInSafetyMonitor" @change="onChange"> Include in Safety Monitor
                      </label>
                  </div>
                  <button @click="removeCondition(idx)" class="btn-danger">Remove</button>
              </div>
          </div>
          <p v-else class="card-description">No conditions configured - IsSafe always reports true.</p>

          <div class="add-condition-row">
              <select v-model="newCategory">
                  <option v-for="m in METRICS" :key="m.id" :value="m.id">{{ m.label }}</option>
              </select>
              <template v-if="selectedCategory?.kind === 'numeric'">
                  <select v-model="newOperator">
                      <option v-for="o in OPERATORS" :key="o.id" :value="o.id">{{ o.label }}</option>
                  </select>
                  <input type="number" step="0.1" v-model.number="newThreshold" :placeholder="selectedCategory.unit">
              </template>
              <select v-else v-model="newEventMetric">
                  <option v-for="e in selectedCategory?.events" :key="e.id" :value="e.id">{{ e.label }}</option>
              </select>
              <label><input type="checkbox" v-model="newNotify"> Notify</label>
              <label v-if="showIncludeCheckbox"><input type="checkbox" v-model="newIncludeInSafetyMonitor"> Include in Safety Monitor</label>
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
    /* Fixed width regardless of how many checkboxes this row actually renders (event rows like
       "Connection: Restored" only show "Notify", not "Include in Safety Monitor") - without this,
       a narrower row here shifts its own "Notify" checkbox rightward to stay flush against the
       Remove button, so it visibly zig-zags out of column alignment with rows that show both
       checkboxes. */
    min-width: 280px;
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
