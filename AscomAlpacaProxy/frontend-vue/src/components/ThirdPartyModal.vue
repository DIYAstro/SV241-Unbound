<script setup>
import { thirdPartyLicenses } from '../data/thirdPartyLicenses'

defineEmits(['close'])
</script>

<template>
  <div class="modal-overlay" @click.self="$emit('close')">
    <div class="modal-content third-party-modal">
      <h3>Third Party Tools</h3>
      <p class="card-description">Open-source components bundled into this application's compiled binary and web UI.</p>

      <div class="third-party-scroll">
        <div v-for="group in thirdPartyLicenses" :key="group.category" class="tp-group">
          <h4 class="tp-category">{{ group.category }}</h4>
          <div v-for="item in group.items" :key="item.name" class="tp-row">
            <span class="tp-name">{{ item.name }}</span>
            <span class="tp-badge">{{ item.license }}</span>
            <a :href="item.url" target="_blank" rel="noopener" class="tp-link">View license ↗</a>
          </div>
        </div>
      </div>

      <div class="button-row" style="margin-top: 1rem;">
        <button @click="$emit('close')" class="btn-secondary">Close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Deliberately does NOT redefine .modal-overlay/.modal-content - see LicenseModal.vue. */
.third-party-modal {
    max-width: 640px;
    width: 90%;
}

.third-party-scroll {
    max-height: 55vh;
    overflow-y: auto;
    padding-right: 0.25rem;
}

.tp-group + .tp-group {
    margin-top: 1.25rem;
}

.tp-category {
    margin: 0 0 0.5rem;
    font-size: 0.85rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-muted);
}

.tp-row {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.4rem 0;
    border-bottom: 1px solid var(--surface-border);
    flex-wrap: wrap;
}

.tp-name {
    flex: 1 1 auto;
    color: var(--text-primary);
}

.tp-badge {
    font-size: 0.7rem;
    padding: 0.15rem 0.55rem;
    border-radius: 999px;
    background: var(--surface-hover);
    color: var(--text-secondary);
    white-space: nowrap;
}

.tp-link {
    font-size: 0.8rem;
    color: var(--primary-color);
    text-decoration: none;
    white-space: nowrap;
}

.tp-link:hover {
    text-decoration: underline;
}
</style>
