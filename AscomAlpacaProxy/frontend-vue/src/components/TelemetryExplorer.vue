<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import zoomPlugin from 'chartjs-plugin-zoom';
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
  zoomPlugin
)

const store = useDeviceStore()
const { config, switchNames } = storeToRefs(store)
const emit = defineEmits(['close'])

// Stats & Selection
const startDate = ref('')
const endDate = ref('')
const selectedSensors = ref([]) // No default selection
const chartRef = ref(null) // Chart reference for reset zoom
const chartContainerRef = ref(null) // Container ref for ResizeObserver
let resizeObserver = null

// Mapping from internal keys to short keys (for config.ps lookup)
const switchMapping = {
    "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
    "usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
}

// Helper to get custom name with fallback
function getLabel(key, defaultLabel) {
    const customName = switchNames.value?.[key];
    if (customName && customName !== key) {
        return `${defaultLabel} (${customName})`;
    }
    return defaultLabel;
}

// Helper to check if a switch is disabled
function isSwitchDisabled(key) {
    const shortKey = switchMapping[key];
    if (!shortKey) return false;
    
    // Check power startup states
    if (config.value?.ps && config.value.ps[shortKey] === 2) return true;
    
    // Check heater modes
    if (key === 'pwm1' && config.value?.dh?.[0]?.m === 5) return true;
    if (key === 'pwm2' && config.value?.dh?.[1]?.m === 5) return true;
    
    return false;
}

// Dynamic sensor list - filters disabled features and uses custom names
// Axis grouping: y_left = temps/humidity/PWM, y_right = voltage/current/power
const availableSensors = computed(() => {
    const sensors = [
        // Core telemetry (always available) - distinct warm/cool colors
        // Right axis: Voltage, Current, Power (electrical measurements)
        { id: 'voltage', label: 'Voltage (V)', color: '#ffd700', axis: 'y_right' },       // Gold
        { id: 'current', label: 'Current (mA)', color: '#ff8c00', axis: 'y_right' },      // Dark Orange
        { id: 'current_a', label: 'Current (A)', color: '#ff6b00', axis: 'y_right' },     // Deep Orange
        { id: 'power', label: 'Power (W)', color: '#dc143c', axis: 'y_right' },           // Crimson
        // Left axis: Temperatures, Humidity (environmental measurements)
        { id: 't_amb', label: 'Amb Temp (°C)', color: '#00ced1', axis: 'y_left' },       // Dark Turquoise
        { id: 'h_amb', label: 'Humidity (%)', color: '#1e90ff', axis: 'y_left' },        // Dodger Blue
        { id: 'dew_point', label: 'Dew Point (°C)', color: '#9370db', axis: 'y_left' },  // Medium Purple
        { id: 't_lens', label: 'Lens Temp (°C)', color: '#20b2aa', axis: 'y_left' },     // Light Sea Green
    ];
    
    // Switches/Heaters - only add if not disabled - distinct colors
    // PWM on left axis (percentage like humidity), adj_conv on right (voltage), DC/USB are boolean
    const switchSensors = [
        { id: 'pwm1', label: 'PWM 1 (%)', defaultLabel: 'PWM 1 (%)', color: '#ff1493', axis: 'y_left' },      // Deep Pink
        { id: 'pwm2', label: 'PWM 2 (%)', defaultLabel: 'PWM 2 (%)', color: '#00ff7f', axis: 'y_left' },      // Spring Green
        { id: 'dc1', label: 'DC1', defaultLabel: 'DC 1', color: '#e74c3c', isBool: true },    // Red
        { id: 'dc2', label: 'DC2', defaultLabel: 'DC 2', color: '#f39c12', isBool: true },    // Orange
        { id: 'dc3', label: 'DC3', defaultLabel: 'DC 3', color: '#f1c40f', isBool: true },    // Yellow
        { id: 'dc4', label: 'DC4', defaultLabel: 'DC 4', color: '#2ecc71', isBool: true },    // Emerald Green
        { id: 'dc5', label: 'DC5', defaultLabel: 'DC 5', color: '#3498db', isBool: true },    // Blue
        { id: 'usbc12', label: 'USB C1/2', defaultLabel: 'USB C 1/2', color: '#9b59b6', isBool: true }, // Purple
        { id: 'usb345', label: 'USB 3/4/5', defaultLabel: 'USB 3/4/5', color: '#1abc9c', isBool: true }, // Turquoise
        { id: 'adj_conv', label: 'Adj Conv (V)', defaultLabel: 'Adj Conv (V)', color: '#bdc3c7', axis: 'y_right' },       // Silver
    ];
    
    for (const s of switchSensors) {
        if (!isSwitchDisabled(s.id)) {
            sensors.push({
                ...s,
                label: getLabel(s.id, s.defaultLabel)
            });
        }
    }
    
    return sensors;
});

const graphData = ref([])

// Initialize dates to last 24h and setup ResizeObserver
onMounted(() => {
    setPreset('24h')
    
    // Setup ResizeObserver to handle browser zoom and window resize
    if (chartContainerRef.value) {
        resizeObserver = new ResizeObserver(() => {
            if (chartRef.value?.chart) {
                chartRef.value.chart.resize()
            }
        })
        resizeObserver.observe(chartContainerRef.value)
    }
})

// Cleanup ResizeObserver on unmount
onUnmounted(() => {
    if (resizeObserver) {
        resizeObserver.disconnect()
    }
})

function setPreset(preset) {
    const end = new Date();
    let start = new Date();
    
    switch(preset) {
        case '1h': start.setHours(end.getHours() - 1); break;
        case '12h': start.setHours(end.getHours() - 12); break;
        case '24h': start.setHours(end.getHours() - 24); break;
        case '3d': start.setDate(end.getDate() - 3); break;
        case '7d': start.setDate(end.getDate() - 7); break;
    }
    
    // Format for datetime-local: YYYY-MM-DDTHH:mm
    const toLocal = (d) => {
        d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
        return d.toISOString().slice(0, 16);
    }
    
    startDate.value = toLocal(start);
    endDate.value = toLocal(end);
    fetchData();
}

async function fetchData() {
    if (!startDate.value || !endDate.value) return;

    const startTs = new Date(startDate.value).getTime() / 1000;
    const endTs = new Date(endDate.value).getTime() / 1000;

    try {
        const url = `/api/v1/telemetry/history?start=${startTs}&end=${endTs}`;
        const res = await fetch(url);
        if (res.ok) {
            let data = await res.json();
            if(!data) data = [];
            graphData.value = data;
        }
    } catch (e) {
        console.error(e);
    }
}

function downloadCSV() {
    const startTs = new Date(startDate.value).getTime() / 1000;
    const endTs = new Date(endDate.value).getTime() / 1000;
    
    // Map computed columns to their source columns for export
    const exportCols = selectedSensors.value.map(col => {
        if (col === 'current_a') return 'current'; // current_a is computed from current
        return col;
    });
    // Remove duplicates
    const uniqueCols = [...new Set(exportCols)];
    const cols = uniqueCols.join(',');
    
    const url = `/api/v1/telemetry/download?start=${startTs}&end=${endTs}&cols=${cols}`;
    window.location.href = url;
}

function resetZoom() {
    if (chartRef.value?.chart) {
        chartRef.value.chart.resetZoom();
    }
}

const chartData = computed(() => {
    if (!graphData.value || graphData.value.length === 0) return { labels: [], datasets: [] };

    const labels = graphData.value.map(d => {
        const date = new Date(d.t * 1000); // API uses seconds 't'
        return date.toLocaleTimeString([], { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
    });

    const datasets = selectedSensors.value.map(sensorId => {
        const def = availableSensors.value.find(s => s.id === sensorId);
        
        let dataMap = [];
        // Map API keys to sensor ID
        // API: t, v, c, p, temp, hum, dew, lens, pwm1, pwm2
        // What about switches?
        // Wait, 'api.go' implementation of GetHistory maps switches to DataPoint?
        // Checking my internal/telemetry/api.go edit... 
        // I did NOT map switches in GetHistory loop! I commented "ignoring switches".
        // ERROR: If user wants to graph switches, I need to include them in API response.
        // I'll proceed with frontend but I must fix backend API to include switches in JSON.
        
        return {
            // Add axis indicator to label: ← for left, → for right (boolean switches have no suffix)
            label: def ? (def.isBool ? def.label : `${def.label} ${def.axis === 'y_right' ? '→' : '←'}`) : sensorId,
            data: graphData.value.map(d => {
                switch(sensorId) {
                    case 'voltage': return d.v;
                    case 'current': return d.c;
                    case 'current_a': return d.c ? d.c / 1000 : null; // mA to A
                    case 'power': return d.p;
                    case 't_amb': return d.temp;
                    case 'h_amb': return d.hum;
                    case 'dew_point': return d.dew;
                    case 't_lens': return d.lens;
                    case 'pwm1': return d.pwm1;
                    case 'pwm2': return d.pwm2;
                    case 'dc1': return d.dc1;
                    case 'dc2': return d.dc2;
                    case 'dc3': return d.dc3;
                    case 'dc4': return d.dc4;
                    case 'dc5': return d.dc5;
                    case 'usbc12': return d.usbc12;
                    case 'usb345': return d.usb345;
                    case 'adj_conv': return d.adj_conv;
                    default: return 0;
                }
            }),
            borderColor: def ? def.color : '#fff',
            backgroundColor: def && def.isBool ? hexToRgba(def.color, 0.4) : (def ? def.color : '#fff'),
            tension: def && def.isBool ? 0 : 0.3,
            borderWidth: def && def.isBool ? 2 : 2,
            pointRadius: 0,
            pointHitRadius: 10,
            stepped: def && def.isBool ? true : false,
            fill: def && def.isBool ? 'origin' : false,
            yAxisID: def && def.isBool ? 'y_bool' : (def?.axis || 'y_left')
        }
    });

    return { labels, datasets };
})

// Helper for alpha
function hexToRgba(hex, alpha) {
    const r = parseInt(hex.slice(1, 3), 16);
    const g = parseInt(hex.slice(3, 5), 16);
    const b = parseInt(hex.slice(5, 7), 16);
    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
        mode: 'index',
        intersect: false,
    },
    layout: {
        padding: {
            bottom: 25
        }
    },
    scales: {
        y_left: {
            type: 'linear',
            position: 'left',
            beginAtZero: false,
            grid: { color: 'rgba(255,255,255,0.1)' },
            ticks: { color: '#00ced1' }, // Cyan for environmental data
            title: { display: false }
        },
        y_right: {
            type: 'linear',
            position: 'right',
            beginAtZero: false,
            grid: { drawOnChartArea: false }, // Don't draw grid lines for right axis
            ticks: { color: '#ffd700' }, // Gold for electrical data
            title: { display: false }
        },
        y_bool: {
            type: 'linear',
            display: false,
            position: 'right',
            min: 0,
            max: 1 // 1 is 100% height
        },
        x: {
            grid: { color: 'rgba(255,255,255,0.1)' },
            ticks: { maxTicksLimit: 20 }
        }
    },
    plugins: {
        legend: { labels: { color: '#ccc' } },
        zoom: {
            zoom: {
                wheel: { enabled: true },
                pinch: { enabled: true },
                mode: 'x',
            },
            pan: {
                enabled: true,
                mode: 'x',
            },
            limits: {
                x: { min: 'original', max: 'original' }, 
            }
        }
    }
}
</script>

<template>
  <div class="explorer-container">
    <div class="explorer-header">
        <h2>Data Explorer</h2>
        <button class="close-btn" @click="$emit('close')">×</button>
    </div>

    <div class="explorer-body">
        <aside class="explorer-sidebar">
            <div class="control-group">
                <label>Time Range</label>
                <div class="presets">
                    <button @click="setPreset('1h')">1h</button>
                    <button @click="setPreset('12h')">12h</button>
                    <button @click="setPreset('24h')">24h</button>
                    <button @click="setPreset('7d')">7d</button>
                </div>
                <input type="datetime-local" v-model="startDate">
                <input type="datetime-local" v-model="endDate">
                <button class="apply-btn" @click="fetchData">Load Data</button>
            </div>

            <div class="control-group">
                <label>Sensors</label>
                <div class="sensor-list">
                    <div v-for="s in availableSensors" :key="s.id" class="sensor-item">
                        <input type="checkbox" :id="s.id" :value="s.id" v-model="selectedSensors">
                        <label :for="s.id" :style="{color: s.color}">{{ s.label }}</label>
                    </div>
                </div>
            </div>

            <button class="download-btn" @click="downloadCSV">Download Selection CSV</button>
        </aside>

        <section class="explorer-chart" ref="chartContainerRef">
            <div class="chart-toolbar">
                <button class="reset-btn" @click="resetZoom" title="Reset to full view">🔄 Reset View</button>
            </div>
            <Line ref="chartRef" :data="chartData" :options="chartOptions" />
        </section>
    </div>
  </div>
</template>

<style scoped>
.explorer-container {
    position: fixed;
    top: 0; left: 0; right: 0; bottom: 0;
    background: rgba(18, 18, 18, 0.98);
    backdrop-filter: blur(10px);
    z-index: 2000;
    display: flex;
    flex-direction: column;
    color: #fff;
}
.explorer-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.5rem 1rem;
    background: rgba(255,255,255,0.05);
    border-bottom: 1px solid rgba(255,255,255,0.1);
}
.explorer-header h2 {
    margin: 0;
    font-size: 1.1rem;
}
.close-btn {
    background: none; border: none; color: #fff; font-size: 1.5rem; cursor: pointer;
}
.explorer-body {
    flex: 1;
    display: flex;
    overflow: hidden;
}
.explorer-sidebar {
    width: 300px;
    padding: 1.5rem;
    background: rgba(0,0,0,0.2);
    border-right: 1px solid rgba(255,255,255,0.1);
    display: flex; flex-direction: column;
    gap: 2rem;
    overflow-y: auto;
}
.explorer-chart {
    flex: 1;
    padding: 1rem;
    position: relative;
}
.control-group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
}
.presets {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
}
.presets button {
    flex: 1;
    padding: 0.25rem;
    font-size: 0.8rem;
    background: rgba(255,255,255,0.1);
    border: none; color: #fff; cursor: pointer;
}
.presets button:hover { background: rgba(255,255,255,0.2); }
.apply-btn {
    margin-top: 0.5rem;
    padding: 0.5rem;
    background: #007bff; border: none; color: white; cursor: pointer;
    font-weight: bold;
}
.sensor-list {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    max-height: 400px;
    overflow-y: auto;
}
.sensor-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.9rem;
}
.download-btn {
    margin-top: auto;
    padding: 0.75rem;
    background: #28a745;
    border: none; color: #fff;
    cursor: pointer;
    border-radius: 4px;
}
.chart-toolbar {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 0.5rem;
}
.reset-btn {
    padding: 0.4rem 0.8rem;
    background: #555;
    border: 1px solid #777;
    color: #fff;
    cursor: pointer;
    border-radius: 4px;
    font-size: 0.85rem;
}
.reset-btn:hover {
    background: #666;
}
input[type="datetime-local"] {
    background: #333; border: 1px solid #555; color: #fff; padding: 0.25rem;
}
</style>
