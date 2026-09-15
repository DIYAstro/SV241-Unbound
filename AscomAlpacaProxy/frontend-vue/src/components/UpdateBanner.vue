<script setup>
import { ref, onMounted, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useDeviceStore } from '../stores/device'
import { useFlasherStore } from '../stores/flasher'

const showBanner = ref(false)
const installedVersion = ref('')
const bundledVersion = ref('')

const deviceStore = useDeviceStore()
const { firmwareVersion } = storeToRefs(deviceStore)
const flasher = useFlasherStore()

onMounted(async () => {
    await checkFirmwareUpdate()
})

// Re-check whenever the device store's own polled firmware version changes - in particular,
// right after FirmwareFlasher.vue refreshes it on a successful flash, so this banner hides itself
// without needing a page reload.
watch(firmwareVersion, () => { checkFirmwareUpdate() })

async function checkFirmwareUpdate() {
    try {
        // Single call covers both installed and bundled version (previously two separate
        // fetches, the second of which - a direct /flasher/firmware/version.json request - no
        // longer exists now that the standalone flasher page is gone; see
        // internal/flasher.GetInfo for how it derives both).
        const res = await fetch('/api/v1/flash/info')
        if (!res.ok) return
        const info = await res.json()

        installedVersion.value = info.installedVersion || ''
        bundledVersion.value = info.bundledVersion || ''
        if (!installedVersion.value || installedVersion.value.toLowerCase() === 'unknown') {
            showBanner.value = false
            return
        }

        const bundledKnown = bundledVersion.value && bundledVersion.value.toLowerCase() !== 'unknown'
        showBanner.value = bundledKnown && !info.upToDate
    } catch (e) {
        // Device not connected or error - don't show banner
        showBanner.value = false
    }
}

function goToFlasher() {
    flasher.open()
}
</script>

<template>
  <div v-if="showBanner" class="update-banner">
      <span><i class="ri-error-warning-line"></i> Firmware update available: {{ installedVersion }} → {{ bundledVersion }}</span>
      <a href="#" @click.prevent="goToFlasher">Update Now</a>
  </div>
</template>

<style scoped>
.update-banner {
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 1rem;
    background: linear-gradient(90deg, rgba(255, 152, 0, 0.15), rgba(255, 152, 0, 0.25));
    border: 1px solid #ff9800;
    border-radius: var(--radius-sm, 8px);
    padding: 0.75rem 1.5rem;
    margin-bottom: 1rem;
    font-size: 0.9rem;
}

.update-banner a {
    color: #fff;
    background: #ff9800;
    padding: 0.4rem 1rem;
    border-radius: 4px;
    font-weight: 600;
    text-decoration: none;
}

.update-banner a:hover {
    background: #e68900;
    box-shadow: 0 0 10px rgba(255, 152, 0, 0.5);
}
</style>
