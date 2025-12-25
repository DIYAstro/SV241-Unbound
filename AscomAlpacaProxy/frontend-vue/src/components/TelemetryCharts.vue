<script setup>
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'
import { computed, ref } from 'vue'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  TimeScale
} from 'chart.js'
import { Line } from 'vue-chartjs'
// import 'chartjs-adapter-date-fns'; // Not installed, using string labels currently.
// Actually, simple index-based or string labels might be enough if we don't install an adapter. 
// Let's use simple labels from the timestamp string for now to avoid extra dependencies if possible.

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend
)

const store = useDeviceStore()
const { telemetryHistory } = storeToRefs(store)

// Compute Chart Data
const chartData = computed(() => {
    if (!telemetryHistory.value || telemetryHistory.value.length === 0) return { labels: [], datasets: [] };

    // Downsample if too many points?
    // Assuming backend returns reasonable amount.
    
    const labels = telemetryHistory.value.map(d => {
        const date = new Date(d.timestamp);
        return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    });

    return {
        labels,
        datasets: [
            {
                label: 'Ambient Temp (°C)',
                borderColor: '#4bc0c0', // Cyan
                backgroundColor: '#4bc0c0',
                data: telemetryHistory.value.map(d => d.t_amb),
                yAxisID: 'y'
            },
            {
                label: 'Lens Temp (°C)',
                borderColor: '#ff6384', // Red
                backgroundColor: '#ff6384',
                data: telemetryHistory.value.map(d => d.t_lens),
                yAxisID: 'y'
            },
            {
                label: 'Dew Point (°C)',
                borderColor: '#36a2eb', // Blue
                backgroundColor: '#36a2eb',
                data: telemetryHistory.value.map(d => d.d),
                yAxisID: 'y',
                borderDash: [5, 5]
            },
            {
                label: 'Humidity (%)',
                borderColor: '#ffce56', // Yellow
                backgroundColor: '#ffce56',
                data: telemetryHistory.value.map(d => d.h_amb),
                yAxisID: 'y1'
            }
        ]
    }
})

const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
        mode: 'index',
        intersect: false,
    },
    scales: {
        y: {
            type: 'linear',
            display: true,
            position: 'left',
            title: { display: true, text: 'Temperature (°C)' }
        },
        y1: {
            type: 'linear',
            display: true,
            position: 'right',
            grid: {
                drawOnChartArea: false,
            },
            title: { display: true, text: 'Humidity (%)' }
        }
    },
    plugins: {
        legend: {
            labels: { color: '#ccc' }
        }
    }
}
</script>

<template>
  <div class="glass-panel" style="margin-top: 1.5rem; padding: 1rem; min-height: 400px;">
      <h3>Telemetry History (Last 12h)</h3>
      <div class="chart-container">
          <Line v-if="telemetryHistory.length > 0" :data="chartData" :options="chartOptions" />
          <div v-else class="no-data">
              Loading history or no data available...
          </div>
      </div>
  </div>
</template>

<style scoped>
.chart-container {
    height: 350px;
    width: 100%;
}
.no-data {
    display: flex;
    justify-content: center;
    align-items: center;
    height: 100%;
    color: var(--text-muted);
}
</style>
