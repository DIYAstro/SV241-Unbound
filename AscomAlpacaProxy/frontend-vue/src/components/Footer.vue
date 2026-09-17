<script setup>
import { ref } from 'vue'
import LicenseModal from './LicenseModal.vue'
import ThirdPartyModal from './ThirdPartyModal.vue'

const showLicense = ref(false)
const showThirdParty = ref(false)
</script>

<template>
  <footer class="app-footer">
    <button class="footer-link" @click="showLicense = true">License</button>
    <span class="footer-sep">·</span>
    <button class="footer-link" @click="showThirdParty = true">Third Party Tools</button>
  </footer>

  <LicenseModal v-if="showLicense" @close="showLicense = false" />
  <ThirdPartyModal v-if="showThirdParty" @close="showThirdParty = false" />
</template>

<style scoped>
.app-footer {
    margin-top: 2rem;
    padding-top: 1rem;
    border-top: 1px solid var(--surface-border);
    text-align: center;
    font-size: 0.8rem;
    color: var(--text-muted);
}

.footer-link {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    text-decoration: underline dotted;
    cursor: pointer;
}

.footer-link:hover {
    color: var(--text-secondary);
}

.footer-sep {
    margin: 0 0.6rem;
}
</style>
