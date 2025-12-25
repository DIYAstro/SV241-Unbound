<script setup>
import { ref, onMounted } from 'vue'
import { useDeviceStore } from '../stores/device'
import { storeToRefs } from 'pinia'

const store = useDeviceStore()
const { proxyConfig } = storeToRefs(store)

const showModal = ref(false)
const status = ref('Initializing...')
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
    let installedVersion = null

    // Poll for firmware connection
    for (let elapsed = 0; elapsed < maxWaitSeconds; elapsed += pollIntervalMs / 1000) {
        const remaining = maxWaitSeconds - elapsed
        status.value = `Waiting for device... (${remaining}s)`

        try {
            const fwRes = await fetch('/api/v1/firmware/version')
            if (fwRes.ok) {
                const fwData = await fwRes.json()
                if (fwData.version && fwData.version.toLowerCase() !== 'unknown') {
                    installedVersion = fwData.version
                    break
                }
            }
        } catch (e) {
            // Device not connected yet
        }

        await new Promise(resolve => setTimeout(resolve, pollIntervalMs))
    }

    if (installedVersion) {
        // Firmware connected - check for update
        try {
            const bundledRes = await fetch('/flasher/firmware/version.json')
            const bundledData = await bundledRes.json()
            const bundledVersion = bundledData.version

            if (installedVersion === bundledVersion) {
                status.value = `✅ SV241-Unbound firmware detected.\nVersion: ${installedVersion}`
                actions.value = [
                    { label: 'Continue Setup', primary: true, handler: completeOnboarding }
                ]
            } else {
                status.value = `⚠ Firmware update available.\nInstalled: ${installedVersion} → Available: ${bundledVersion}`
                actions.value = [
                    { label: 'Update Firmware', primary: true, handler: releaseAndFlash },
                    { label: 'Skip', primary: false, handler: completeOnboarding }
                ]
            }
        } catch (e) {
            status.value = `✅ SV241-Unbound firmware detected.\nVersion: ${installedVersion}`
            actions.value = [
                { label: 'Continue Setup', primary: true, handler: completeOnboarding }
            ]
        }
    } else {
        // No firmware detected
        status.value = `⚠ Compatible firmware not detected.\nPlease flash SV241-Unbound to get started.`
        actions.value = [
            { label: 'Flash Firmware', primary: true, handler: releaseAndFlash },
            { label: "I'll do it later", primary: false, handler: completeOnboarding }
        ]
    }
}

async function releaseAndFlash() {
    try {
        await fetch('/api/serial/release', { method: 'POST' })
    } catch (e) { /* ignore */ }
    window.location.href = '/flasher'
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
      <div class="onboarding-modal glass-panel">
          <h2>Welcome to SV241-Unbound</h2>
          <p class="subtitle">Let's get your device set up</p>
          
          <div class="status-display">
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
.modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.7);
    display: flex;
    justify-content: center;
    align-items: center;
    z-index: 1000;
}

.onboarding-modal {
    padding: 2.5rem;
    max-width: 500px;
    width: 90%;
    text-align: center;
}

.onboarding-modal h2 {
    margin: 0 0 0.5rem 0;
    background: linear-gradient(90deg, #fff, var(--primary-color));
    background-clip: text;
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
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
