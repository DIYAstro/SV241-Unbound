<script setup>
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'
import { computed, ref, onMounted, onUnmounted, nextTick, watch } from 'vue'

const store = useDeviceStore()
const { activeSwitches, switchNames, powerStatus, config, proxyConfig, liveStatus, isConnected } = storeToRefs(store)

const masterPowerState = computed({
    get: () => {
        // If all visible switches are on, Master is on.
        if (Object.keys(powerStatus.value).length === 0) return false;
        
        // Guard against empty list (every() returns true on empty)
        if (visibleSwitches.value.length === 0) return false; 

        return visibleSwitches.value.every(s => isSwitchOn(s.shortKey));
    },
    set: (val) => {
        if (!isConnected.value) return;
        store.setAllPower(val);
    }
})

const switchMapping = {
    "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
    "usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
}

function isSwitchOn(key) {
    const val = powerStatus.value[key];
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
    if (!isConnected.value) return;
    store.setSwitch(id, !currentState);
}

// Truncation detection
const truncatedSwitches = ref(new Set());
const switchRefs = ref([]);

function checkTruncation() {
    truncatedSwitches.value.clear();
    switchRefs.value.forEach((el, index) => {
        if (el) {
            const nameEl = el.querySelector('.name');
            if (nameEl) {
                // scrollWidth/clientWidth are both rounded to whole pixels, which can lose a
                // borderline sub-pixel overflow entirely - both then round to the *same* integer,
                // so no tolerance added to that comparison can recover the lost precision.
                // Chromium/Edge and Firefox don't always round the same borderline case the same
                // way, so a name could correctly show the ellipsis in both (that part's pure CSS,
                // sub-pixel precise) while this scrollWidth/clientWidth check only catches it in
                // one of them. A Range over the text's own content reports its actual (sub-pixel
                // precise) rendered width regardless of the parent's overflow:hidden clipping -
                // comparing that against the equally precise getBoundingClientRect() of the
                // (possibly clipped) container avoids integer rounding on either side.
                const range = document.createRange();
                range.selectNodeContents(nameEl);
                const fullWidth = range.getBoundingClientRect().width;
                const visibleWidth = nameEl.getBoundingClientRect().width;
                if (fullWidth > visibleWidth + 0.5) {
                    truncatedSwitches.value.add(index);
                }
            }
        }
    });
    // Force reactivity update
    truncatedSwitches.value = new Set(truncatedSwitches.value);
}

function isTruncated(index) {
    return truncatedSwitches.value.has(index);
}

// Check truncation on mount and when switches change
onMounted(() => {
    nextTick(checkTruncation);
    window.addEventListener('resize', checkTruncation);
    document.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
    window.removeEventListener('resize', checkTruncation);
    document.removeEventListener('click', handleClickOutside);
});

watch(visibleSwitches, () => nextTick(checkTruncation), { deep: true });

// --- Manual Control Logic (PWM & Voltage) ---
const popoverOpen = ref(null); // 'pwm1' or 'pwm2' or 'adj_conv'
const sliderValue = ref(0); // Local value

function isVariableControl(s) {
    // 1. Manual PWM
    if (s.key === 'pwm1' && powerStatus.value.dm && powerStatus.value.dm[0] === 0) return true;
    if (s.key === 'pwm2' && powerStatus.value.dm && powerStatus.value.dm[1] === 0) return true;
    
    // 2. Variable Voltage (if enabled)
    if (s.key === 'adj_conv' && proxyConfig.value.enableAlpacaVoltageControl) return true;
    
    return false;
}

function getControlParams(key) {
    if (key === 'adj_conv') {
        return { min: 0, max: 15.0, step: 0.1, unit: 'V' };
    }
    // Default PWM
    return { min: 0, max: 100, step: 1, unit: '%' };
}

function getDisplayValue(key) {
    const val = getPwmValue(key);
    const params = getControlParams(key);
    // If unit is V, force 1 decimal
    if (params.unit === 'V') return val.toFixed(1) + params.unit;
    return val + params.unit;
}

function getPwmValue(key) {
    if (popoverOpen.value === key) return sliderValue.value;
    
    // Map long key to short key (e.g. 'adj_conv' -> 'adj')
    const lookupKey = switchMapping[key] || key;
    
    // special handling for 'adj': it resides in powerStatus (target value), not liveStatus
    if (lookupKey === 'adj') {
        if (!powerStatus.value) return 0;
        return powerStatus.value[lookupKey] || 0;
    }
    
    if (!liveStatus.value) return 0;
    return liveStatus.value[lookupKey] || 0;
}

function togglePopover(key, event) {
    if (!isConnected.value) return;
    if (popoverOpen.value === key) {
        closePopover();
    } else {
        const lookupKey = switchMapping[key] || key;
        
        let initialVal = 0;
        if (lookupKey === 'adj') {
             initialVal = powerStatus.value ? (powerStatus.value[lookupKey] || 0) : 0;
        } else {
             initialVal = liveStatus.value ? (liveStatus.value[lookupKey] || 0) : 0;
        }
        
        sliderValue.value = initialVal;
        popoverOpen.value = key;
    }
    event.stopPropagation();
}

function closePopover() {
    popoverOpen.value = null;
}

function onSliderInput(event) {
    sliderValue.value = parseFloat(event.target.value);
}

function commitPwmValue(id) {
    // Send the value to the backend ONLY when OK is clicked
    store.setSwitchValue(id, sliderValue.value);
    closePopover();
}

function handleClickOutside(event) {
    if (popoverOpen.value) {
        const popovers = document.querySelectorAll('.pwm-popover');
        let clickedInside = false;
        popovers.forEach(p => {
            if (p.contains(event.target)) clickedInside = true;
        });
        if (!clickedInside) {
            closePopover();
        } 
    }
}
</script>

<template>
  <div id="live-power-control" class="glass-panel card full-width">
      <h2>Power Control</h2>
      <!-- Master Switch - only show if enableMasterPower is true -->
      <div v-if="proxyConfig.enableMasterPower !== false" id="master-switch-container" class="switch-row master-row">
          <span class="name" id="master-power-label">{{ proxyConfig.switchNames?.master_power || 'Master Power' }}</span>
          <label class="switch-toggle neon-toggle" :class="{ 'disabled': !isConnected }">
              <input type="checkbox" v-model="masterPowerState" :disabled="!isConnected">
              <span class="slider"></span>
          </label>
      </div>
      <div id="power-grid" class="power-grid">
          <div v-for="(s, index) in visibleSwitches" :key="s.id" 
               :ref="el => switchRefs[index] = el"
               class="switch-control glass-panel" 
               :data-fullname="isTruncated(index) ? s.name : ''">
              <span class="name">{{ s.name }}</span>

              <!-- Wrapper to ensure stable layout on right side -->
              <div class="control-wrapper">
                  <!-- Manual/Variable Control (Badge + Popover) -->
                  <div v-if="isVariableControl(s)" class="pwm-manual-container">
                      <div class="pwm-badge" 
                           :class="{ 'is-off': !s.isOn, 'disabled': !isConnected }"
                           @click="togglePopover(s.key, $event)" 
                           title="Adjust Value">
                          {{ s.isOn ? getDisplayValue(s.key) : 'OFF' }}
                      </div>
                      
                      <div v-if="popoverOpen === s.key" 
                           class="pwm-popover" 
                           @click.stop>
                          <div class="popover-header">
                              <span>Power</span>
                              <span class="value">{{ getDisplayValue(s.key) }}</span>
                          </div>
                          <div class="slider-wrapper">
                              <input type="range" 
                                     :min="getControlParams(s.key).min" 
                                     :max="getControlParams(s.key).max" 
                                     :step="getControlParams(s.key).step"
                                     :value="sliderValue" 
                                     @input="onSliderInput"
                                     class="pwm-slider">
                              <button class="ok-btn" @click="commitPwmValue(s.id)">OK</button>
                          </div>
                      </div>
                  </div>

                  <!-- Standard Toggle Switch -->
                  <label v-else class="switch-toggle" :class="{ 'disabled': !isConnected }">
                      <input type="checkbox" :checked="s.isOn" @change="toggleSwitch(s.id, s.isOn)" :disabled="!isConnected">
                      <span class="slider"></span>
                  </label>
              </div>
          </div>
      </div>
  </div>
</template>

<style scoped>
/* Allow tooltip to overflow the card */
/* .switch-control definition moved below to pair with control-wrapper */


/* Stable wrapper for the right-side control */
.control-wrapper {
    /* Fixed geometry strategy: Always reserve space */
    width: 6.0rem; 
    flex-shrink: 0; /* Never shrink */
    display: flex;
    justify-content: flex-end; /* Align content to right */
    align-items: center;
}

/* Ensure text doesn't overlap */
.switch-control {
    overflow: visible;
    position: relative;
    display: flex; /* Enforce flex layout */
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem; /* Minimum gap */
    /* padding-right: 0; REMOVED to restore natural padding */
}

/* Text truncation for long switch names */
.switch-control .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: default;
}

/* Tooltip wrapper on the card itself */
.switch-control::before {
    content: attr(data-fullname);
    position: absolute;
    left: 1.2rem;
    top: -2.5rem;
    padding: 0.5rem 0.75rem;
    /* Same fix as .pwm-popover below - was hardcoded to the old theme's colors, and --surface-color
       alone (no blur backdrop behind it here either) was too translucent over other cards' text -
       see .pwm-popover's comment. */
    background: var(--bg-color-start);
    border: 1px solid var(--surface-border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 0.85rem;
    white-space: nowrap;
    opacity: 0;
    visibility: hidden;
    transition: opacity 0.2s ease, visibility 0.2s ease;
    pointer-events: none;
    z-index: 1000;
    box-shadow: var(--shadow-soft);
}


.switch-control[data-fullname]:not([data-fullname=""]):hover::before {
    opacity: 1;
    visibility: visible;
    transition-delay: 0.7s;
}

/* PWM Manual Control Styles */
.pwm-manual-container {
    position: relative;
    /* margin-right: 0.5rem; handled by layout usually, flex auto? */
    display: flex;
    align-items: center;
    /* margin-left: auto; REMOVED - Handled by .control-wrapper */
}

.pwm-badge {
    /* Was hardcoded neon-cyan-on-purple regardless of theme - now tied to the same
       --primary-color/--primary-glow/--surface-* variables the rest of the app (style.css,
       AppModal.vue) already uses, so this actually follows whichever theme is selected. */
    background: var(--primary-glow);
    color: var(--primary-color);
    border: 1px solid var(--primary-glow);
    padding: 0.15rem 0.5rem;
    border-radius: 4px;
    font-size: 1.0rem;
    font-family: inherit;
    cursor: pointer;
    transition: all 0.2s ease;
    user-select: none;
    text-shadow: 0 0 5px var(--primary-glow);
    min-width: 4.5rem;
    text-align: center;
}

.pwm-badge.is-off {
    background: var(--surface-color);
    color: var(--text-secondary);
    border-color: var(--surface-border);
    text-shadow: none;
}

.pwm-badge:hover {
    background: var(--primary-glow);
    box-shadow: 0 0 8px var(--primary-glow);
    transform: translateY(-1px);
}
.pwm-badge.is-off:hover {
    background: var(--surface-hover);
    box-shadow: 0 0 5px var(--surface-border);
    color: var(--text-primary);
}

.pwm-popover {
    position: absolute;
    bottom: 120%; /* Above the badge */
    right: 0; /* Align right edge */
    width: 280px; /* Slightly wider for Button */
    /* --surface-color (the .glass-panel recipe) turned out too translucent here - that's built
       for large panels sitting on the smooth page gradient, not a small popover floating on top
       of other, busier foreground content (other switch cards/text), and .modal-content's
       equally-light background only works because it always has --modal-overlay's near-opaque
       backdrop behind it, which this popover doesn't have. --bg-color-start is solid/opaque in
       every theme and dark in all three, giving reliable contrast against anything behind it -
       matches what the original hardcoded rgba(15, 12, 41, 0.95) was already going for, just
       theme-aware now. */
    background: var(--bg-color-start);
    border: 1px solid var(--surface-border);
    border-radius: 12px;
    padding: 1.25rem;
    z-index: 2000; /* Above regular tooltips */
    box-shadow: var(--shadow-soft);
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
    animation: fadeIn 0.15s ease-out;
    transform-origin: bottom right;
}

.popover-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.95rem;
    color: var(--text-secondary);
}

/* OK Button Styles */
.ok-btn {
    background: var(--primary-glow);
    color: var(--primary-color);
    border: 1px solid var(--primary-glow);
    border-radius: 4px;
    padding: 0.2rem 0.6rem;
    font-family: inherit;
    font-size: 0.9rem;
    cursor: pointer;
    transition: all 0.2s;
    text-shadow: 0 0 5px var(--primary-glow);
}

.ok-btn:hover {
    background: var(--primary-glow);
    box-shadow: 0 0 8px var(--primary-glow);
    transform: translateY(-1px);
}

.ok-btn:active {
    transform: translateY(0);
}

.slider-wrapper {
    display: flex;
    align-items: center;
    gap: 0.8rem;
    padding-top: 0.5rem;
}

.popover-header .value {
    color: var(--primary-color);
    font-weight: bold;
    font-size: 1.1rem;
    text-shadow: 0 0 5px var(--primary-glow);
}

/* Slider Styling */
.pwm-slider {
    -webkit-appearance: none;
    width: 100%;
    height: 4px;
    background: var(--surface-hover);
    border-radius: 2px;
    outline: none;
    flex: 1; /* Take remaining space */
}

.pwm-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--primary-color);
    cursor: pointer;
    box-shadow: 0 0 10px var(--primary-glow);
    transition: transform 0.1s;
}

.pwm-slider::-webkit-slider-thumb:hover {
    transform: scale(1.1);
}

/* Disabled State Styling */
.disabled {
    opacity: 0.5;
    pointer-events: none;
    cursor: not-allowed;
}
.switch-toggle.disabled .slider {
    background-color: #555;
    cursor: not-allowed;
}

@keyframes fadeIn {
    from { opacity: 0; transform: translateY(10px); }
    to { opacity: 1; transform: translateY(0); }
}
</style>
