<script setup>
import { useDeviceStore } from '../../stores/device'
import { useModalStore } from '../../stores/modal'
import { storeToRefs } from 'pinia'
import { ref, computed } from 'vue'

const store = useDeviceStore()
const modal = useModalStore()
const { activeSwitches, switchNames, config, proxyConfig } = storeToRefs(store)

// Mappings
const switchMapping = {
    "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
    "usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
}

// Edit State (Overrides store state)
const edits = ref({}) // Keyed by 'key' (dc1, etc.) -> { startupState, currentName, rawValue }

function getEdit(key) {
    if (!edits.value[key]) edits.value[key] = {};
    return edits.value[key];
}

const hasChanges = computed(() => Object.keys(edits.value).length > 0);

function getDefaultName(key) {
    const map = {
        "dc1": "DC 1", "dc2": "DC 2", "dc3": "DC 3", "dc4": "DC 4", "dc5": "DC 5",
        "usbc12": "USB-C 1/2", "usb345": "USB 3/4/5", "adj_conv": "Adj. Port",
        "pwm1": "PWM 1", "pwm2": "PWM 2",
    };
    return map[key] || key;
}

// Computed Display Rows - Shows ALL switches for configuration (not filtered by activeSwitches)
const displayRows = computed(() => {
    const rows = []
    // User requested EXCLUSIVE config in Heater tab, so removing PWM from here
    const orderedKeys = ["dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv"];
    
    for (const key of orderedKeys) {
        // Find ID from activeSwitches if exists, otherwise use key as placeholder
        const id = activeSwitches.value 
            ? Object.keys(activeSwitches.value).find(k => activeSwitches.value[k] === key) || key
            : key;

        const shortKey = switchMapping[key] || key;
        
        // 1. Get Base Values from Store
        const storeName = switchNames.value[key] || getDefaultName(key);
        let storeState = 0;
        
        // Special handling for PWM startup state (mapped to dh.en)
        if (key === 'pwm1' && config.value.dh && config.value.dh[0]) {
             storeState = config.value.dh[0].en ? 1 : 0;
             if (config.value.dh[0].m === 5) storeState = 2; // Disabled
        } else if (key === 'pwm2' && config.value.dh && config.value.dh[1]) {
             storeState = config.value.dh[1].en ? 1 : 0;
             if (config.value.dh[1].m === 5) storeState = 2; // Disabled
        } else {
             // Standard switches in ps
             storeState = config.value.ps ? config.value.ps[shortKey] : 0;
        }

        let storeRawValue = undefined;
        let storeVoltage = '-';

        if (key === 'adj_conv') {
            storeRawValue = (config.value.av !== undefined) ? config.value.av : 0;
            storeVoltage = storeRawValue + ' V';
        } else if (key === 'usbc12' || key === 'usb345' || key.startsWith('dc')) {
            storeVoltage = '-';
        }

        // 2. Override with Edits if present
        const edit = edits.value[key] || {};
        
        const currentName = (edit.currentName !== undefined) ? edit.currentName : storeName;
        const startupState = (edit.startupState !== undefined) ? edit.startupState : storeState;
        const rawValue = (edit.rawValue !== undefined) ? edit.rawValue : storeRawValue;
        
        // Voltage display logic using MERGED value
        let voltage = storeVoltage; 
        // Note: For adj_conv, voltage display depends on rawValue.
        if (key === 'adj_conv') {
             voltage = rawValue + ' V';
        }

        rows.push({
            id,
            key,
            shortKey,
            defaultName: getDefaultName(key),
            currentName,
            startupState, // This binds to the select
            voltage,
            rawValue,
            isPwm: (key === 'pwm1' || key === 'pwm2') // Flag for UI
        })
    }
    return rows;
})


// Change Handlers updates 'edits' map
function onNameChange(key, val) {
    const edit = getEdit(key);
    edit.currentName = val;
}

function onStateChange(key, val) {
    if (key === 'pwm1' || key === 'pwm2') {
        modal.info("Please configure heater startup state in the 'Heaters' tab.", 'Heater Configuration');
        return; // Prevent editing here to avoid conflict with HeaterConfig
    }
    const edit = getEdit(key);
    edit.startupState = parseInt(val);
}

function onVoltageChange(key, val) {
    const edit = getEdit(key);
    edit.rawValue = parseFloat(val);
}


async function save() {
    let saved = false;
    
    const newPs = config.value.ps ? { ...config.value.ps } : {};
    let newAv = config.value.av;
    
    const newNames = { ...store.proxyConfig.switchNames };
    
    let psChanged = false;
    let namesChanged = false;

    // Apply edits to reconstructed config objects
    for (const key of Object.keys(edits.value)) {
        const edit = edits.value[key];
        const shortKey = switchMapping[key] || key;
        
        if (edit.startupState !== undefined) {
             newPs[shortKey] = edit.startupState;
             psChanged = true;
        }
        
        if (edit.rawValue !== undefined && key === 'adj_conv') {
            newAv = edit.rawValue;
            psChanged = true;
        }
        
        if (edit.currentName !== undefined) {
            newNames[key] = edit.currentName;
            namesChanged = true;
        }
    }

    if (psChanged) {
        try {
            await store.saveConfig({ ps: newPs, av: newAv });
            saved = true;
        } catch (e) {
            modal.error('Error saving startup states: ' + e.message);
            return;
        }
    }

    if (namesChanged) {
        try {
             const payload = { ...store.proxyConfig, switchNames: newNames };
             await store.saveProxyConfig(payload);
             saved = true;
        } catch (e) {
            modal.error('Error saving names: ' + e.message);
            return;
        }
    }
    
    if (saved) {
        modal.success('Configuration saved.');
        edits.value = {}; // Clear edits on success
    } else {
        modal.info('No changes detected.');
    }
}

</script>

<template>
  <div class="config-group full-width-group">
      <h3>Switch Configuration</h3>
      <p class="subtitle">Configure switch names, startup states, and visibility.</p>

      <div class="table-container">
          <table class="config-table">
              <thead>
                  <tr>
                      <th class="th-name">Switch Name</th>
                      <th class="th-state">State (Startup)</th>
                      <th class="th-custom">Custom Name</th>
                      <th class="th-volt">Voltage</th>
                  </tr>
              </thead>
              <tbody>
                  <tr v-for="row in displayRows" :key="row.key">
                      <td>{{ row.defaultName }}</td>
                      <td>
                          <select :value="row.startupState" @change="e => onStateChange(row.key, e.target.value)">
                              <option value="0">OFF</option>
                              <option value="1">ON</option>
                              <option value="2">Disabled</option>
                          </select>
                      </td>
                      <td>
                          <input type="text" :value="row.currentName" @input="e => onNameChange(row.key, e.target.value)" :placeholder="row.defaultName">
                      </td>
                      <td>
                          <div v-if="row.key === 'adj_conv'" style="display: flex; align-items: center; gap: 0.5rem;">
                              <input type="number" :value="row.rawValue" @input="e => onVoltageChange(row.key, e.target.value)" step="0.1" min="0" max="15" style="width: 80px;">
                              <span>V</span>
                          </div>
                          <span v-else>{{ row.voltage }}</span>
                      </td>
                  </tr>
              </tbody>
          </table>
      </div>

      <button @click="save" class="btn-primary" style="margin-top: 1rem; width: 100%;" :disabled="!hasChanges">Save Switch Configuration</button>
  </div>
</template>

<style scoped>
/* Scoped styles */
</style>
