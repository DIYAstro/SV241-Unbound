import { computed } from 'vue'

// Every metric the proxy currently caches per live sensor poll (serial.Conditions.Data) and
// therefore can evaluate a condition against - see internal/serial's safetyMetricExtractors,
// which this list must stay in sync with. The two "event" entries don't have a live numeric
// value/threshold at all - they represent discrete, edge-triggered occurrences (see
// internal/serial's fireSafetyEvent) and use a second "event" dropdown instead of operator+value.
export const METRICS = [
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
export const OPERATORS = [
    { id: '>', label: '>' },
    { id: '>=', label: '≥' },
    { id: '<', label: '<' },
    { id: '<=', label: '≤' },
]

// Flat label lookup for event-metric rows (they have no operator/threshold to render from).
export const EVENT_METRIC_LABELS = {
    connectionLost: 'Connection: Lost',
    connectionRestored: 'Connection: Restored',
    heaterLimitEngaged: 'Heater Current-Limit: Engaged',
    heaterLimitCleared: 'Heater Current-Limit: Cleared',
}
export function isEventMetric(metric) {
    return metric in EVENT_METRIC_LABELS
}

// Only the two "bad state" events can meaningfully be included in the ASCOM SafetyMonitor
// calculation - "restored"/"cleared" are momentary recoveries with nothing to include (see
// internal/serial's applyConnectionLostSafetyState/booleanSafetyMetrics for how these two are
// actually evaluated, which differs between them since polling stops entirely on disconnect).
export const SAFETY_MONITOR_ELIGIBLE_EVENTS = ['connectionLost', 'heaterLimitEngaged']

export function metricInfo(id) {
    return METRICS.find(m => m.id === id) || { label: id, unit: '' }
}
export function operatorLabel(id) {
    return OPERATORS.find(o => o.id === id)?.label || id
}

// Mirrors internal/serial's safetyMetricExtractors - must stay in sync with that map.
export function extractMetricValue(metric, status) {
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

export function evaluateCondition(operator, value, threshold) {
    if (typeof value !== 'number') return false
    switch (operator) {
        case '>': return value > threshold
        case '>=': return value >= threshold
        case '<': return value < threshold
        case '<=': return value <= threshold
        default: return false
    }
}

export function formatValue(value, unit) {
    return typeof value === 'number' ? `${value.toFixed(1)} ${unit}` : '--'
}

// Enriches a reactive list of saved conditions with each one's current live value and whether
// it's the one (or one of several) currently tripping "unsafe" - shared by SafetyConfig.vue (the
// Conditions list/editor) and LiveTelemetry.vue (the at-a-glance status), so both read the exact
// same computation instead of drifting apart over time.
export function useSafetyConditionStatuses(conditions, liveStatus, isConnected) {
    const conditionStatuses = computed(() => (conditions.value || []).map(cond => {
        const value = extractMetricValue(cond.metric, liveStatus.value || {})
        return {
            ...cond,
            currentValue: value,
            triggered: isConnected.value && evaluateCondition(cond.operator, value, cond.threshold)
        }
    }))
    const triggeredConditions = computed(() => conditionStatuses.value.filter(c => c.triggered))
    return { conditionStatuses, triggeredConditions }
}
