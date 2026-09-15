<script setup>
import { ref, onMounted } from 'vue'
import { useDeviceStore } from '../stores/device'
import { useFlasherStore } from '../stores/flasher'
import { storeToRefs } from 'pinia'

const store = useDeviceStore()
const { proxyConfig } = storeToRefs(store)
const flasher = useFlasherStore()

const showModal = ref(false)
const status = ref('Initializing...')
const statusIcon = ref('')
const actions = ref([]) // Array of { label, primary, handler }

onMounted(async () => {
    // Check if first run is already complete
    await store.checkConnection()
    
    if (proxyConfig.value?.firstRunComplete) {
        return // Already completed onboarding
    }
    
    showModal.value = true
    await runOnboarding()
})

async function runOnboarding() {
    const maxWaitSeconds = 15
    const pollIntervalMs = 2000
    let info = null

    // Poll for firmware connection - a single /api/v1/flash/info call now covers both installed
    // and bundled version (previously two separate fetches, the second of which - a direct
    // /flasher/firmware/version.json request - no longer exists now that the standalone flasher
    // page is gone; see internal/flasher.GetInfo for how it derives both).
    for (let elapsed = 0; elapsed < maxWaitSeconds; elapsed += pollIntervalMs / 1000) {
        const remaining = maxWaitSeconds - elapsed
        status.value = `Waiting for device... (${remaining}s)`

        try {
            const res = await fetch('/api/v1/flash/info')
            if (res.ok) {
                const data = await res.json()
                if (data.installedVersion && data.installedVersion.toLowerCase() !== 'unknown') {
                    info = data
                    break
                }
            }
        } catch (e) {
            // Device not connected yet
        }

        await new Promise(resolve => setTimeout(resolve, pollIntervalMs))
    }

    if (info) {
        // Firmware connected - check for update. A bundled version GetInfo couldn't determine
        // (e.g. a local dev build) must not be presented as a mismatch - same reasoning as
        // FirmwareFlasher.vue's own "unable to compare" case.
        const bundledKnown = info.bundledVersion && info.bundledVersion.toLowerCase() !== 'unknown'
        if (!bundledKnown || info.upToDate) {
            statusIcon.value = 'ri-checkbox-circle-line'
            status.value = `SV241-Unbound firmware detected.\nVersion: ${info.installedVersion}`
            actions.value = [
                { label: 'Continue Setup', primary: true, handler: completeOnboarding }
            ]
        } else {
            statusIcon.value = 'ri-error-warning-line'
            status.value = `Firmware update available.\nInstalled: ${info.installedVersion} → Available: ${info.bundledVersion}`
            actions.value = [
                { label: 'Update Firmware', primary: true, handler: releaseAndFlash },
                { label: 'Skip', primary: false, handler: completeOnboarding }
            ]
        }
    } else {
        // No firmware detected
        statusIcon.value = 'ri-error-warning-line'
        status.value = `This device doesn't have SV241-Unbound firmware installed yet.\nClick below to flash it now.`
        actions.value = [
            { label: 'Flash Firmware', primary: true, handler: releaseAndFlash },
            { label: "I'll do it later", primary: false, handler: completeOnboarding }
        ]
    }
}

async function releaseAndFlash() {
    // Hide this wizard and persist firstRunComplete directly (same fields completeOnboarding()
    // saves) WITHOUT its reload - a reload here would tear down the flasher modal we're about to
    // open along with it. The flasher modal takes over from here; there's no need for this
    // wizard to reappear once it's done or cancelled. It only takes exclusive control of the
    // serial port once a flash actually starts (see internal/flasher.StartFlash /
    // serial.AcquirePortForFlashing), not just from opening.
    showModal.value = false
    try {
        const currentConfig = proxyConfig.value || {}
        currentConfig.firstRunComplete = true
        await store.saveProxyConfig(currentConfig)
    } catch (e) {
        console.error('Failed to save onboarding status', e)
    }
    flasher.open()
}

async function completeOnboarding() {
    try {
        const currentConfig = proxyConfig.value || {}
        currentConfig.firstRunComplete = true
        await store.saveProxyConfig(currentConfig)
    } catch (e) {
        console.error("Failed to save onboarding status", e)
    }
    window.location.reload()
}
</script>

<template>
  <div v-if="showModal" class="modal-overlay">
      <div class="onboarding-modal modal-content">
          <h2>Welcome to SV241-Unbound</h2>
          <p class="subtitle">Let's get your device set up</p>
          
          <div class="status-display">
              <i v-if="statusIcon" :class="statusIcon" class="status-icon"></i>
              <pre>{{ status }}</pre>
          </div>
          
          <div class="actions" v-if="actions.length">
              <button 
                  v-for="(action, i) in actions" 
                  :key="i"
                  :class="action.primary ? 'btn-primary' : 'btn-secondary'"
                  @click="action.handler">
                  {{ action.label }}
              </button>
          </div>
      </div>
  </div>
</template>

<style scoped>
.onboarding-modal {
    max-width: 500px;
    width: 90%;
    text-align: center;
}

.onboarding-modal h2 {
    margin: 0 0 0.5rem 0;
    color: var(--primary-color);
}

.subtitle {
    color: var(--text-secondary);
    margin-bottom: 1.5rem;
}

.status-display {
    background: rgba(0, 0, 0, 0.3);
    border-radius: 8px;
    padding: 1.5rem;
    margin-bottom: 1.5rem;
    white-space: pre-wrap;
    text-align: left;
    font-size: 0.95rem;
}

.status-icon {
    display: block;
    text-align: center;
    font-size: 1.5rem;
    color: var(--primary-color);
    margin-bottom: 0.5rem;
}

.actions {
    display: flex;
    gap: 1rem;
    justify-content: center;
    flex-wrap: wrap;
}

.actions button {
    min-width: 140px;
}
</style>
