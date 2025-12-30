<script setup>
import { onMounted, ref } from 'vue'
import Header from './components/Header.vue'
import UpdateBanner from './components/UpdateBanner.vue'
import OnboardingWizard from './components/OnboardingWizard.vue'
import LiveTelemetry from './components/LiveTelemetry.vue'
import TelemetryExplorer from './components/TelemetryExplorer.vue'
import PowerControl from './components/PowerControl.vue'
import Configuration from './components/Configuration.vue'
import LiveLog from './components/LiveLog.vue'
import AppModal from './components/AppModal.vue'
import { useDeviceStore } from './stores/device'

const store = useDeviceStore()
const showExplorer = ref(false)

onMounted(() => {
    store.startPolling()
})
</script>

<template>
  <div class="container">
    <!-- Global Modal -->
    <AppModal />
    <!-- Onboarding Wizard (shows on first run) -->
    <OnboardingWizard />
    
    <Header />
    
    <!-- Firmware Update Banner -->
    <UpdateBanner />
    
    <main>
      <div class="dashboard-grid">
        <LiveTelemetry @open-explorer="showExplorer = true" />
        <PowerControl />
      </div>
      
      <TelemetryExplorer v-if="showExplorer" @close="showExplorer = false" />

      <div class="section-spacing">
        <Configuration />
      </div>
      
      <!-- Live Log Section -->
      <div class="section-spacing">
        <LiveLog />
      </div>
    </main>
  </div>
</template>

<style scoped>
.section-spacing {
    margin-top: 1.5rem;
}
</style>
