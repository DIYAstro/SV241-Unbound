<script setup>
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'
import { computed } from 'vue'

const store = useDeviceStore()
const { activeSwitches, switchNames, powerStatus, config, proxyConfig } = storeToRefs(store)

const masterPowerState = computed({
    get: () => {
        // If all visible switches are on, Master is on. Or check internal logic?
        // App.js logic: checked = allOn.
        // We iterate visible switches.
        if (Object.keys(powerStatus.value).length === 0) return false;
        return visibleSwitches.value.every(s => isSwitchOn(s.key));
    },
    set: (val) => {
        store.setAllPower(val);
    }
})

const switchMapping = {
    "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
    "usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
}

function isSwitchOn(key) {
    const val = powerStatus.value[key];
    // Check if value is "truthy" (1 or boolean true or > 0 for voltage)
    return (typeof val === 'boolean' && val) || (typeof val === 'number' && val > 0);
}

const visibleSwitches = computed(() => {
    const switches = [];
    if (!activeSwitches.value) return [];

    for (const [id, key] of Object.entries(activeSwitches.value)) {
        if (key === 'master_power' || key.startsWith('sensor_')) continue;

        const shortKey = switchMapping[key] || key;

        // Filter out Disabled switches (Config.ps[shortKey] === 2)
        if (config.value.ps && config.value.ps[shortKey] === 2) continue;

        // Filter out Disabled heaters (Config.dh[0 or 1].m === 5)
        if (config.value.dh) {
            if (key === 'pwm1' && config.value.dh[0] && config.value.dh[0].m === 5) continue;
            if (key === 'pwm2' && config.value.dh[1] && config.value.dh[1].m === 5) continue;
        }

        const name = switchNames.value[key] || getDefaultName(key);
        switches.push({
            id: id,
            key: key, // internal key e.g. "dc1"
            shortKey: shortKey, // key in status JSON e.g. "d1"
            name: name,
            isOn: isSwitchOn(shortKey) // Use shortKey for looking up status
        });
    }
    return switches;
});

function getDefaultName(key) {
    const map = {
        "dc1": "DC 1", "dc2": "DC 2", "dc3": "DC 3", "dc4": "DC 4", "dc5": "DC 5",
        "usbc12": "USB (C/1/2)", "usb345": "USB (3/4/5)", "adj_conv": "Adj. Voltage",
        "pwm1": "PWM 1", "pwm2": "PWM 2",
    };
    return map[key] || key;
}

function toggleSwitch(id, currentState) {
    store.setSwitch(id, !currentState);
}
</script>

<template>
  <div id="live-power-control" class="glass-panel card full-width">
      <h2>Power Control</h2>
      <!-- Master Switch - only show if enableMasterPower is true -->
      <div v-if="proxyConfig.enableMasterPower !== false" id="master-switch-container" class="switch-row master-row">
          <span class="name" id="master-power-label">{{ proxyConfig.switchNames?.master_power || 'Master Power' }}</span>
          <label class="switch-toggle neon-toggle">
              <input type="checkbox" v-model="masterPowerState">
              <span class="slider"></span>
          </label>
      </div>
      <div id="power-grid" class="power-grid">
          <div v-for="s in visibleSwitches" :key="s.id" class="switch-control glass-panel">
              <span class="name" :title="s.name">{{ s.name }}</span>
              <label class="switch-toggle">
                  <input type="checkbox" :checked="s.isOn" @change="toggleSwitch(s.id, s.isOn)">
                  <span class="slider"></span>
              </label>
          </div>
      </div>
  </div>
</template>

<style scoped>
/* Text truncation for long switch names */
.switch-control .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
</style>
