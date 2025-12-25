<script setup>
import { ref, onMounted } from 'vue'

const showBanner = ref(false)
const installedVersion = ref('')
const bundledVersion = ref('')

onMounted(async () => {
    await checkFirmwareUpdate()
})

async function checkFirmwareUpdate() {
    try {
        // Fetch installed version from API
        const fwRes = await fetch('/api/v1/firmware/version')
        if (!fwRes.ok) return // Device not connected
        
        const fwData = await fwRes.json()
        installedVersion.value = fwData.version
        if (!installedVersion.value || installedVersion.value.toLowerCase() === 'unknown') return

        // Fetch bundled version
        const bundledRes = await fetch('/flasher/firmware/version.json')
        if (!bundledRes.ok) return
        
        const bundledData = await bundledRes.json()
        bundledVersion.value = bundledData.version

        // Show banner if versions mismatch
        if (bundledVersion.value && installedVersion.value !== bundledVersion.value) {
            showBanner.value = true
        }
    } catch (e) {
        // Device not connected or error - don't show banner
    }
}

function goToFlasher() {
    window.location.href = '/flasher'
}
</script>

<template>
  <div v-if="showBanner" class="update-banner">
      <span>⚠️ Firmware update available: {{ installedVersion }} → {{ bundledVersion }}</span>
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
    color: #ff9800;
    font-weight: 600;
    text-decoration: none;
}

.update-banner a:hover {
    text-decoration: underline;
}
</style>
