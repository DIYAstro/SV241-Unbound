<script setup>
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'

const store = useDeviceStore()
const { liveStatus, proxyConfig } = storeToRefs(store)

const emit = defineEmits(['open-explorer'])
</script>

<template>
  <div class="glass-panel">
    <div class="panel-header">
        <h2>Live Telemetry</h2>
        <!-- Only show Data Explorer button if telemetry logging is enabled -->
        <button v-if="proxyConfig.telemetryInterval > 0" class="icon-btn" @click="$emit('open-explorer')" title="Open Data Explorer">
            📊
        </button>
    </div>

    <div class="telemetry-grid">
        <!-- Voltage -->
        <div class="telemetry-item">
        <span class="label">Voltage</span>
        <span class="value" id="status-v">
            {{ (liveStatus.v || 0).toFixed(2) }} <small>V</small>
        </span>
        </div>

        <!-- Current -->
        <div class="telemetry-item">
        <span class="label">Current</span>
        <span class="value" id="status-i">
            {{ (liveStatus.i && liveStatus.i !== 0 ? liveStatus.i / 1000 : 0).toFixed(2) }} <small>A</small>
        </span>
        </div>

        <!-- Power -->
        <div class="telemetry-item">
        <span class="label">Power</span>
        <span class="value" id="status-p">
            {{ (liveStatus.p || 0).toFixed(2) }} <small>W</small>
        </span>
        </div>

        <!-- Ambient Temp -->
        <div class="telemetry-item">
        <span class="label">Amb Temp</span>
        <span class="value" id="status-t_amb">
            {{ (liveStatus.t_amb || 0).toFixed(1) }} <small>°C</small>
        </span>
        </div>

        <!-- Humidity -->
        <div class="telemetry-item">
        <span class="label">Humidity</span>
        <span class="value" id="status-h_amb">
            {{ (liveStatus.h_amb || 0).toFixed(1) }} <small>%</small>
        </span>
        </div>

        <!-- Dew Point -->
        <div class="telemetry-item">
        <span class="label">Dew Point</span>
        <span class="value" id="status-d">
            {{ (liveStatus.d || 0).toFixed(1) }} <small>°C</small>
        </span>
        </div>

        <!-- Lens Temp -->
        <div class="telemetry-item">
        <span class="label">Lens Temp</span>
        <span class="value" id="status-t_lens">
            {{ (liveStatus.t_lens || 0).toFixed(1) }} <small>°C</small>
        </span>
        </div>

        <!-- PWM 1 -->
        <div class="telemetry-item">
        <span class="label">PWM 1</span>
        <span class="value" id="status-pwm1">
            {{ Math.round(liveStatus.pwm1 || 0) }} <small>%</small>
        </span>
        </div>

        <!-- PWM 2 -->
        <div class="telemetry-item">
        <span class="label">PWM 2</span>
        <span class="value" id="status-pwm2">
            {{ Math.round(liveStatus.pwm2 || 0) }} <small>%</small>
        </span>
        </div>
    </div>
  </div>
</template>

<style scoped>
.panel-header {
    display: flex;
    justify-content: flex-start;
    align-items: center;
    gap: 1rem;
    margin-bottom: 0.5rem;
    padding: 1rem 1rem 0 1rem; /* Align with grid padding */
}
.icon-btn {
    background: none; border: 1px solid rgba(255,255,255,0.2);
    color: #fff; padding: 0.25rem 0.5rem;
    border-radius: 4px; cursor: pointer;
    font-size: 1.2rem;
}
.icon-btn:hover { background: rgba(255,255,255,0.1); }

.telemetry-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(100px, 1fr)); /* Responsive columns */
  gap: 1rem;
  padding: 1rem;
  text-align: center;
}

.telemetry-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 0.5rem;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
  transition: background 0.2s, transform 0.2s;
  /* Not clickable anymore */
}

/* Hover effect purely visual now, or remove? Keeping vague hover for better feel but removing pointer */
.telemetry-item:hover {
  background: rgba(255, 255, 255, 0.1);
  /* transform: translateY(-2px); remove move effect to imply non-interactivity */
}

.label {
  font-size: 0.8rem;
  color: var(--text-muted);
  margin-bottom: 0.25rem;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.value {
  font-size: 1.25rem;
  font-weight: 600;
  color: var(--text-color);
}

.value small {
  font-size: 0.8rem;
  font-weight: 400;
  color: var(--text-muted);
  margin-left: 2px;
}
</style>
