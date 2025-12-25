<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, computed, watch, onMounted } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { config, proxyConfig, switchNames } = storeToRefs(store)

const localHeaters = ref([])
// We need local state for names and auto-enable settings to allow editing before saving
const localNames = ref({}) 
const localAutoEnable = ref({})
const hasChanges = ref(false)

// Modes: 0:Manual, 1:PID(Lens), 2:Ambient, 3:Sync, 4:MinTemp, 5:Disabled
const heaterModes = [
    { val: 0, label: "Manual" },
    { val: 1, label: "PID (Lens Sensor)" },
    { val: 2, label: "Ambient Tracking" },
    { val: 3, label: "PID-Sync (Follower)" },
    { val: 4, label: "Minimum Temperature" },
    { val: 5, label: "Disabled" },
]

// Initialize local state from store config
watch(() => config.value.dh, (newVal) => {
    if (newVal) {
        // Deep copy safely AND convert `en` to boolean (firmware sends 0/1)
        localHeaters.value = newVal.map(h => ({
            ...h,
            en: !!h.en  // Convert 1 -> true, 0 -> false
        }));
        
        // Sync names and auto-enable if not already edited
        if (!hasChanges.value) {
            localNames.value = {
                'pwm1': switchNames.value['pwm1'] || '',
                'pwm2': switchNames.value['pwm2'] || ''
            };
            
            const ae = proxyConfig.value.heaterAutoEnableLeader || {};
            localAutoEnable.value = {
                'pwm1': ae['pwm1'] || false,
                'pwm2': ae['pwm2'] || false
            };
        }
    }
}, { immediate: true, deep: true })

// Also watch proxy config for initial load if it comes later
watch(() => proxyConfig.value, (newVal) => {
    if (newVal && !hasChanges.value) {
         localNames.value = {
            'pwm1': newVal.switchNames?.['pwm1'] || '',
            'pwm2': newVal.switchNames?.['pwm2'] || ''
        };
        const ae = newVal.heaterAutoEnableLeader || {};
        localAutoEnable.value = {
            'pwm1': ae['pwm1'] || false,
            'pwm2': ae['pwm2'] || false
        };
    }
}, { deep: true });


function onChange() {
    hasChanges.value = true;
}

// Helper to get heater key
function getHeaterKey(index) {
    return index === 0 ? 'pwm1' : 'pwm2';
}

function updateHeader(index) {
    // Only used for display binding
}

function isOptionDisabled(heaterIndex, optionVal) {
    const otherIndex = heaterIndex === 0 ? 1 : 0;
    const otherMode = localHeaters.value[otherIndex]?.m;
    const exclusiveModes = [1, 4];

    if (optionVal === 3) {
         // Sync only allowed if other is 1 or 4
         return !(otherMode === 1 || otherMode === 4);
    }
    
    if (exclusiveModes.includes(optionVal)) {
        // If other IS 1 or 4, this option is disabled
        return exclusiveModes.includes(otherMode);
    }
    return false;
}

async function save() {
    // 1. Prepare Firmware Payload (dh)
    const dhPayload = {
        dh: localHeaters.value.map(h => ({
            m: parseInt(h.m),
            en: h.en ? 1 : 0,  // Convert boolean to 0/1 for firmware
            mp: parseFloat(h.mp) || 0,
            to: parseFloat(h.to) || 0,
            kp: parseFloat(h.kp) || 0,
            ki: parseFloat(h.ki) || 0,
            kd: parseFloat(h.kd) || 0,
            sd: parseFloat(h.sd) || 0,
            ed: parseFloat(h.ed) || 0,
            xp: parseFloat(h.xp) || 0,
            psf: parseFloat(h.psf) || 0,
            mt: parseFloat(h.mt) || 0
        }))
    };
    
    // 2. Prepare Proxy Payload (Names + AutoEnable)
    // We must merge with existing proxy config to avoid data loss
    // Explicitly unwrap refs to avoid passing RefImpl objects
    const currentSwitchNames = switchNames.value || {}; // storeToRefs
    const currentProxyConfig = proxyConfig.value || {}; // storeToRefs

    const newSwitchNames = { ...currentSwitchNames, ...localNames.value };
    
    // Safety check for heaterAutoEnableLeader existence
    const currentAutoEnable = currentProxyConfig.heaterAutoEnableLeader || {};
    const newAutoEnable = { ...currentAutoEnable, ...localAutoEnable.value };
    
    const proxyPayload = {
        ...currentProxyConfig,
        switchNames: newSwitchNames,
        heaterAutoEnableLeader: newAutoEnable
    };
    
    // Debug log to ensure payload is valid
    console.log("Saving Proxy Payload:", proxyPayload);

    try {
        // Save Firmware Settings
        await store.saveConfig(dhPayload);
        
        // Save Proxy Settings
        await store.saveProxyConfig(proxyPayload);

        modal.success('Heater settings and names saved.');
        hasChanges.value = false;
    } catch (e) {
        modal.error('Error saving: ' + e.message);
    }
}
</script>

<template>
  <div class="config-group full-width-group">
      <h3>Dew Heater Configuration</h3>
      <div v-for="(heater, index) in localHeaters" :key="index" class="glass-panel settings-card">
          <!-- Dynamic Header -->
          <h4>{{ localNames[getHeaterKey(index)] || `Heater ${index+1} (PWM${index+1})` }}</h4>
          
          <!-- Custom Name Input -->
          <div class="form-group">
              <label>Custom Name (Proxy)</label>
              <input type="text" 
                     v-model="localNames[getHeaterKey(index)]" 
                     @input="onChange" 
                     :disabled="heater.m === 5"
                     placeholder="e.g. Primary Scope">
          </div>

          <!-- Enable on Startup - hide for Mode 5 (Disabled) -->
          <div class="form-group checkbox-group" v-if="heater.m !== 5">
            <label>
                <input type="checkbox" v-model="heater.en" @change="onChange">
                Enable on Startup
            </label>
          </div>
          
           <!-- Auto Enable Leader - only show for Mode 3 (Follower/Sync) -->
           <div class="form-group checkbox-group" v-if="heater.m === 3">
            <label title="Automatically enable the leader heater when this follower starts?">
                <input type="checkbox" 
                       v-model="localAutoEnable[getHeaterKey(index)]" 
                       @change="onChange">
                Auto-Enable Leader (Proxy)
            </label>
          </div>

          <div class="form-group">
              <label>Mode</label>
              <select v-model.number="heater.m" @change="e => { onChange(); if(heater.m === 5) { heater.en = false; localNames[getHeaterKey(index)] = ''; } }">
                  <option v-for="opt in heaterModes" :key="opt.val" :value="opt.val" :disabled="isOptionDisabled(index, opt.val)">
                      {{ opt.label }}
                  </option>
              </select>
          </div>

          <!-- Settings Fields based on Mode -->
          
          <!-- Mode 0: Manual -->
          <div v-if="heater.m == 0" class="mode-settings active">
              <label>Power (%)</label>
              <input type="number" v-model.number="heater.mp" min="0" max="100" @input="onMetricChange">
          </div>

          <!-- Mode 1: PID -->
          <div v-if="heater.m == 1" class="mode-settings active">
              <label>Target Offset (°C)</label>
              <input type="number" v-model.number="heater.to" step="0.1" @input="onMetricChange">
              <div class="pid-grid">
                  <label>Kp <input type="number" v-model.number="heater.kp" step="0.1" @input="onMetricChange"></label>
                  <label>Ki <input type="number" v-model.number="heater.ki" step="0.1" @input="onMetricChange"></label>
                  <label>Kd <input type="number" v-model.number="heater.kd" step="0.1" @input="onMetricChange"></label>
              </div>
          </div>

          <!-- Mode 2: Ambient -->
          <div v-if="heater.m == 2" class="mode-settings active">
               <div class="pid-grid">
                  <label>Start Delta (°C) <input type="number" v-model.number="heater.sd" step="0.1" @input="onMetricChange"></label>
                  <label>End Delta (°C) <input type="number" v-model.number="heater.ed" step="0.1" @input="onMetricChange"></label>
                  <label>Max Power (%) <input type="number" v-model.number="heater.xp" min="0" max="100" @input="onMetricChange"></label>
               </div>
          </div>

          <!-- Mode 3: Sync -->
          <div v-if="heater.m == 3" class="mode-settings active">
              <label>Sync Factor</label>
              <input type="number" v-model.number="heater.psf" step="0.1" min="0" max="2.0" @input="onMetricChange">
              <!-- Auto-enable leader checkbox is in Proxy config, skip for now or fetch separately -->
          </div>

          <!-- Mode 4: Min Temp -->
          <div v-if="heater.m == 4" class="mode-settings active">
              <label>Min Temp (°C)</label>
              <input type="number" v-model.number="heater.mt" step="0.1" @input="onMetricChange">
              <label>Target Offset (°C)</label>
              <input type="number" v-model.number="heater.to" step="0.1" @input="onMetricChange">
              <div class="pid-grid">
                  <label>Kp <input type="number" v-model.number="heater.kp" step="0.1" @input="onMetricChange"></label>
                  <label>Ki <input type="number" v-model.number="heater.ki" step="0.1" @input="onMetricChange"></label>
                  <label>Kd <input type="number" v-model.number="heater.kd" step="0.1" @input="onMetricChange"></label>
              </div>
          </div>

      </div>
      
      <button @click="save" class="btn-primary" style="margin-top: 1rem; width: 100%;" :disabled="!hasChanges">
          Save Heater Settings
      </button>
  </div>
</template>

<style scoped>
.settings-card {
    padding: 1.25rem;
    margin-bottom: 1rem;
}

.settings-card h4 {
    margin: 0 0 1rem 0;
    color: var(--primary-color);
    font-weight: 600;
}

.mode-settings {
    margin-top: 1rem;
    padding-top: 1rem;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
}

.pid-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0.5rem;
    margin-top: 0.5rem;
}

label {
    display: block;
    font-size: 0.85rem;
    color: var(--text-secondary);
    margin-bottom: 0.2rem;
}

input[type="number"], select {
    width: 100%;
}
</style>
