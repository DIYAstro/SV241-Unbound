<script setup>
import { ref, onMounted } from 'vue'
import { useModalStore } from '../../stores/modal'

const modal = useModalStore()
const config = ref({
    enableWeatherService: false,
    weatherLatitude: 0,
    weatherLongitude: 0,
    weatherModel: 'best_match',
    weatherInterval: 5,
    weatherSourcePriority: {}
})

const metrics = [
    { id: 'temperature', label: 'Temperature' },
    { id: 'humidity', label: 'Humidity' },
    { id: 'dewpoint', label: 'Dew Point' },
    { id: 'pressure', label: 'Pressure' },
    { id: 'windspeed', label: 'Wind Speed' },
    { id: 'winddirection', label: 'Wind Direction' },
    { id: 'windgust', label: 'Wind Gust' },
    { id: 'cloudcover', label: 'Cloud Cover' },
    { id: 'rainrate', label: 'Rain Rate' }
]

const models = [
    { id: 'best_match', label: 'Best Match (Auto)' },
    { id: 'ecmwf_ifs', label: 'ECMWF IFS (Global Standard)' },
    { id: 'icon_seamless', label: 'ICON (Europe/DWD)' },
    { id: 'gfs_seamless', label: 'GFS (Global US)' }
]

async function loadSettings() {
    try {
        const resp = await fetch('/api/v1/settings')
        const data = await resp.json()
        if (data.proxy_config) {
            config.value = data.proxy_config
            // Initialize priorities if empty
            metrics.forEach(m => {
                if (!config.value.weatherSourcePriority[m.id]) {
                    config.value.weatherSourcePriority[m.id] = 'hybrid'
                }
            })
        }
    } catch (e) {
        modal.error('Failed to load weather settings: ' + e.message)
    }
}

async function saveSettings() {
    try {
        const resp = await fetch('/api/v1/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config.value)
        })
        if (!resp.ok) throw new Error(resp.statusText)
        modal.success('Weather settings saved successfully.')
    } catch (e) {
        modal.error('Failed to save settings: ' + e.message)
    }
}

function detectLocation() {
    if (!navigator.geolocation) {
        modal.error('Geolocation is not supported by your browser.')
        return
    }

    modal.show({
        title: 'Detect Location',
        message: 'The browser will now ask for your location (Lat/Lon) to fetch weather for your exact position.',
        buttons: [
            {
                text: 'Detect Now',
                primary: true,
                action: () => {
                    modal.close()
                    navigator.geolocation.getCurrentPosition(
                        (pos) => {
                            config.value.weatherLatitude = parseFloat(pos.coords.latitude.toFixed(6))
                            config.value.weatherLongitude = parseFloat(pos.coords.longitude.toFixed(6))
                            modal.success('Location detected: ' + config.value.weatherLatitude + ', ' + config.value.weatherLongitude)
                        },
                        (err) => {
                            modal.error('Could not detect location: ' + err.message)
                        }
                    )
                }
            },
            { text: 'Cancel', action: () => modal.close() }
        ]
    })
}

onMounted(loadSettings)
</script>

<template>
  <div class="weather-content animate-in">
    <!-- Main Service Toggle & Settings -->
    <div class="config-group">
      <div class="settings-header">
        <label class="header-toggle" for="enable-weather">
          <input type="checkbox" id="enable-weather" v-model="config.enableWeatherService">
          <h3>Open-Meteo Weather Service</h3>
        </label>
      </div>
      <p class="description">
        Fetch supplemental meteorological data from Open-Meteo to provide missing metrics for ASCOM Observing Conditions.
      </p>

      <div class="settings-grid" :class="{ disabled: !config.enableWeatherService }">
        <div class="setting-item">
          <label>Position (Latitude / Longitude)</label>
          <div class="coord-inputs">
            <input type="number" v-model.number="config.weatherLatitude" step="0.000001" placeholder="Latitude">
            <input type="number" v-model.number="config.weatherLongitude" step="0.000001" placeholder="Longitude">
            <button @click="detectLocation" class="btn-secondary compact center-btn" title="Detect from Browser">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="btn-icon">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="2" y1="12" x2="22" y2="12"></line>
                <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path>
              </svg>
              Detect
            </button>
          </div>
          <span class="input-hint">Convention: Latitude (+N / -S) | Longitude (+E / -W)</span>
        </div>

        <div class="setting-item">
          <label>Prediction Model</label>
          <select v-model="config.weatherModel">
            <option v-for="m in models" :key="m.id" :value="m.id">{{ m.label }}</option>
          </select>
          <span class="input-hint">Default: Best Match for your region.</span>
        </div>

        <div class="setting-item">
          <label>API Poll Interval (Minutes)</label>
          <div class="interval-input">
             <input type="number" v-model.number="config.weatherInterval" min="1" max="60">
             <span class="unit">min</span>
          </div>
          <span class="input-hint">Recommended: 2 - 10 minutes.</span>
        </div>
      </div>
    </div>

    <!-- Priority Matrix -->
    <div class="config-group" :class="{ disabled: !config.enableWeatherService }">
      <div class="title-with-icon">
          <h3>Metric Data Sourcing</h3>
      </div>
      <p class="description">Choose how each ObservingCondition property should be sourced.</p>
      
      <div class="priority-matrix glass-panel">
        <div class="matrix-row header">
          <div class="metric-col">Metric</div>
          <div class="source-col">Source Strategy</div>
        </div>
        <div v-for="m in metrics" :key="m.id" class="matrix-row">
          <div class="metric-col">{{ m.label }}</div>
          <div class="source-col">
            <div class="radio-group">
                <label class="radio-option">
                    <input type="radio" :name="'source-'+m.id" value="hybrid" v-model="config.weatherSourcePriority[m.id]">
                    <span class="radio-label">Hybrid</span>
                </label>
                <label class="radio-option">
                    <input type="radio" :name="'source-'+m.id" value="hardware" v-model="config.weatherSourcePriority[m.id]">
                    <span class="radio-label">Hardware</span>
                </label>
                <label class="radio-option">
                    <input type="radio" :name="'source-'+m.id" value="internet" v-model="config.weatherSourcePriority[m.id]">
                    <span class="radio-label">Internet</span>
                </label>
            </div>
          </div>
        </div>
      </div>
    </div>

    <button @click="saveSettings" class="btn-primary full-width-btn">Save Weather Configuration</button>
  </div>
</template>

<style scoped>
.weather-content {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
}

.header-toggle {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    cursor: pointer;
}

.header-toggle h3 {
    margin: 0;
}

.header-toggle input[type="checkbox"] {
    width: 1.25rem;
    height: 1.25rem;
    accent-color: var(--primary-color);
}

.description {
    font-size: 0.9rem;
    color: var(--text-secondary);
    margin-bottom: 1.25rem;
    line-height: 1.4;
}

.settings-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 1.5rem;
    transition: opacity 0.3s ease;
}

.settings-grid.disabled {
    opacity: 0.4;
    pointer-events: none;
}

.setting-item {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
}

.setting-item label {
    font-weight: 500;
    font-size: 0.9rem;
    color: var(--text-primary);
}

.coord-inputs {
    display: flex;
    gap: 0.5rem;
}

.coord-inputs input {
    flex: 1;
}

.btn-icon {
    width: 1.1rem;
    height: 1.1rem;
}

.center-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
}

.interval-input {
    display: flex;
    align-items: center;
    gap: 0.75rem;
}

.interval-input input {
    width: 80px;
}

.unit {
    color: var(--text-muted);
    font-size: 0.85rem;
}

.input-hint {
    font-size: 0.75rem;
    color: var(--text-muted);
}

/* Priority Matrix Styles */
.priority-matrix {
    margin-top: 0.5rem;
    border-radius: var(--radius-lg);
    overflow: hidden;
}

.matrix-row {
    display: grid;
    grid-template-columns: 140px 1fr;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--surface-border);
    align-items: center;
}

.matrix-row.header {
    background: rgba(255, 255, 255, 0.03);
    font-weight: bold;
    font-size: 0.85rem;
    text-transform: uppercase;
    letter-spacing: 0.5px;
}

.matrix-row:last-child {
    border-bottom: none;
}

.radio-group {
    display: flex;
    gap: 1.5rem;
}

.radio-option {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    font-size: 0.85rem;
}

.radio-option input[type="radio"] {
    margin: 0;
    width: 1rem;
    height: 1rem;
    accent-color: var(--primary-color);
}

.radio-label {
    color: var(--text-secondary);
}

.radio-option input:checked + .radio-label {
    color: var(--primary-color);
    font-weight: 500;
}

.full-width-btn {
    margin-top: 1rem;
    width: 100%;
}

.animate-in {
    animation: fadeInSlide 0.4s ease-out;
}

@keyframes fadeInSlide {
    from { opacity: 0; transform: translateY(10px); }
    to { opacity: 1; transform: translateY(0); }
}

@media (max-width: 600px) {
    .radio-group {
        flex-direction: column;
        gap: 0.5rem;
    }
    .matrix-row {
        grid-template-columns: 1fr;
        gap: 0.5rem;
    }
    .matrix-row.header {
        display: none;
    }
}
</style>
