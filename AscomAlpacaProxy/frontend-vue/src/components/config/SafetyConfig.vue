<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, watch, computed } from 'vue'
import {
    METRICS, OPERATORS, EVENT_METRIC_LABELS, isEventMetric, SAFETY_MONITOR_ELIGIBLE_EVENTS,
    metricInfo, operatorLabel, formatValue, useSafetyConditionStatuses
} from '../../composables/useSafetyMonitor'

const store = useDeviceStore()
const modal = useModalStore()
const { proxyConfig, liveStatus, isConnected } = storeToRefs(store)

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

// Enriched with each condition's current live value and whether it's the one (or one of several)
// currently tripping "unsafe" - drives the per-row highlight below. The at-a-glance SAFE/UNSAFE
// status itself now lives in LiveTelemetry.vue (always visible on the dashboard), not here - see
// that component for the other half of this shared computation.
const { conditionStatuses } = useSafetyConditionStatuses(conditions, liveStatus, isConnected)

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
