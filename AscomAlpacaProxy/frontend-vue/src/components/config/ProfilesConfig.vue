<script setup>
import { useModalStore } from '../../stores/modal'
import { useDeviceStore } from '../../stores/device'
import { ref, onMounted } from 'vue'

const modal = useModalStore()
const store = useDeviceStore()

// --- Save ---
const newProfileName = ref('')
const saving = ref(false)

async function saveProfile() {
    const name = newProfileName.value.trim()
    if (!name) {
        modal.error('Please enter a name for the profile.')
        return
    }
    saving.value = true
    try {
        const response = await fetch('/api/v1/profiles/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name })
        })
        if (!response.ok) throw new Error(response.statusText)
        newProfileName.value = ''
        modal.success(`Profile "${name}" saved.`)
        fetchProfiles()
    } catch (e) {
        modal.error('Failed to save profile: ' + e.message)
    } finally {
        saving.value = false
    }
}

// --- List ---
// Profiles from the currently connected box are shown directly; profiles saved from a different
// (or now-disconnected) box are collapsed behind this toggle - covers the "box was replaced"
// case without cluttering the default view. Mirrors SystemSettings.vue's auto-backup list pattern.
const profiles = ref([])
const loadingProfiles = ref(false)
const showOtherDevices = ref(false)

async function fetchProfiles() {
    loadingProfiles.value = true
    try {
        const response = await fetch('/api/v1/profiles/list')
        if (response.ok) {
            profiles.value = await response.json()
        }
    } catch (e) {
        console.error('Failed to fetch profiles', e)
    } finally {
        loadingProfiles.value = false
    }
}

onMounted(fetchProfiles)

function formatTimestamp(iso) {
    if (!iso) return 'Unknown time'
    return new Date(iso).toLocaleString()
}

function visibleProfiles() {
    return profiles.value.filter(p => showOtherDevices.value || p.matchesCurrentDevice)
}

function otherDeviceCount() {
    return profiles.value.filter(p => !p.matchesCurrentDevice).length
}

// --- Apply ---
// Mirrors SystemSettings.vue's performAutoRestore()/performRestore() for the device-mismatch
// confirmation flow (same 409 response shape). Unlike those, applying a profile does NOT reuse
// applyBackupRestore() on the backend (see server.go's comment on handleApplyProfile) - it's a
// live config merge + device-scoped proxy setters only, same as any normal settings change, so
// there's no reconnect cycle and no reason to prompt for a reboot.
async function performApply(profile, force = false) {
    modal.loading('Please wait while the profile is applied…', 'Applying Profile')
    try {
        const url = `/api/v1/profiles/apply?file=${encodeURIComponent(profile.filename)}${force ? '&force=true' : ''}`
        const response = await fetch(url, { method: 'POST' })

        if (response.status === 409) {
            const mismatch = await response.json()
            const profileLabel = mismatch.backupRigName || mismatch.backupDeviceSerial || 'an unknown box'
            const currentLabel = mismatch.currentRigName || mismatch.currentDeviceSerial || 'the connected box'
            modal.confirm(
                `This profile was saved from "${profileLabel}", but "${currentLabel}" is currently connected. ` +
                `Applying it will overwrite the connected box's on-device settings (calibration, heater configuration, ` +
                `power startup states, etc.) with the ones from the profile. Continue anyway?`,
                {
                    title: 'Different Box Detected',
                    confirmText: 'Apply Anyway',
                    cancelText: 'Cancel',
                    onConfirm: () => performApply(profile, true)
                }
            )
            return
        }

        if (!response.ok) throw new Error(response.statusText)

        // Refresh config immediately - same call saveConfig() already makes after any normal
        // settings change (device.js). Without it, config.value.ps stays stale and anything
        // reading it directly (e.g. SwitchConfig.vue's Disabled badge) would show the old state
        // until the next full page load. activeSwitches/switchNames don't need an explicit
        // refresh here - the store's 2s poll (checkConnection) keeps those current unconditionally.
        await store.fetchConfig()
        modal.success(`Profile "${profile.name}" applied.`, 'Profile Applied')
        fetchProfiles()
    } catch (e) {
        modal.error('Failed to apply profile: ' + e.message)
    }
}

function confirmApply(profile) {
    modal.confirm(
        `This will overwrite your current configuration with profile "${profile.name}". Continue?`,
        {
            title: 'Apply Profile',
            confirmText: 'Apply',
            cancelText: 'Cancel',
            onConfirm: () => performApply(profile)
        }
    )
}

// --- Update (overwrite in place) ---
async function performUpdate(profile) {
    try {
        const response = await fetch(`/api/v1/profiles/update?file=${encodeURIComponent(profile.filename)}`, { method: 'POST' })
        if (!response.ok) throw new Error(response.statusText)
        modal.success(`Profile "${profile.name}" updated with the current configuration.`)
        fetchProfiles()
    } catch (e) {
        modal.error('Failed to update profile: ' + e.message)
    }
}

function confirmUpdate(profile) {
    modal.confirm(
        `This will overwrite profile "${profile.name}" with the current configuration. Continue?`,
        {
            title: 'Update Profile',
            confirmText: 'Update',
            cancelText: 'Cancel',
            onConfirm: () => performUpdate(profile)
        }
    )
}

// --- Delete ---
async function performDelete(profile) {
    try {
        const response = await fetch(`/api/v1/profiles/delete?file=${encodeURIComponent(profile.filename)}`, { method: 'POST' })
        if (!response.ok) throw new Error(response.statusText)
        modal.success(`Profile "${profile.name}" deleted.`)
        fetchProfiles()
    } catch (e) {
        modal.error('Failed to delete profile: ' + e.message)
    }
}

function confirmDelete(profile) {
    modal.confirm(
        `Delete profile "${profile.name}"? This cannot be undone.`,
        {
            title: 'Delete Profile',
            confirmText: 'Delete',
            cancelText: 'Cancel',
            onConfirm: () => performDelete(profile)
        }
    )
}
</script>

<template>
  <div class="config-group full-width-group">
      <h3>Configuration Profiles</h3>

      <div class="action-card glass-panel" style="margin-bottom: 1rem;">
          <h4>Save Current Configuration as Profile</h4>
          <p class="card-description">
              Saves the full current configuration (proxy + firmware) under a name you choose, so
              you can quickly switch back to it later. Unlike on-device profiles on some other
              SV241 firmwares, there's no limit on how many you can keep.
          </p>
          <div class="button-row">
              <input type="text" v-model="newProfileName" placeholder="e.g. Widefield Setup" style="flex: 2;" @keyup.enter="saveProfile">
              <button @click="saveProfile" class="btn-secondary" :disabled="saving">
                  {{ saving ? 'Saving…' : 'Save Profile' }}
              </button>
          </div>
      </div>

      <div class="action-card glass-panel">
          <h4>Saved Profiles</h4>
          <p class="card-description">
              Profiles are tied to the box they were saved from. Applying one pushes the saved
              settings live, the same as a normal settings change - no reboot or reconnect.
          </p>

          <div v-if="visibleProfiles().length" class="profile-list">
              <div v-for="profile in visibleProfiles()" :key="profile.filename" class="profile-row">
                  <span class="profile-info">
                      <strong>{{ profile.name }}</strong>
                      <small>{{ formatTimestamp(profile.savedAt) }}<template v-if="profile.rigName"> – {{ profile.rigName }}</template></small>
                  </span>
                  <div class="button-row profile-actions">
                      <button @click="confirmApply(profile)" class="btn-secondary">Apply</button>
                      <button @click="confirmUpdate(profile)" class="btn-secondary">Update</button>
                      <button @click="confirmDelete(profile)" class="btn-danger">Delete</button>
                  </div>
              </div>
          </div>
          <p v-else class="card-description">
              {{ loadingProfiles ? 'Loading…' : 'No profiles saved yet.' }}
          </p>

          <div v-if="otherDeviceCount() > 0" class="button-row" style="margin-top: 0.75rem;">
              <button @click="showOtherDevices = !showOtherDevices" class="btn-secondary">
                  {{ showOtherDevices ? 'Hide' : 'Show' }} profiles from other devices ({{ otherDeviceCount() }})
              </button>
          </div>

          <div class="button-row" style="margin-top: 0.75rem;">
              <button @click="fetchProfiles" class="btn-secondary" :disabled="loadingProfiles">
                  {{ loadingProfiles ? 'Loading…' : 'Refresh List' }}
              </button>
          </div>
      </div>
  </div>
</template>

<style scoped>
.action-card {
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
}

.action-card h4 {
    margin: 0;
    color: var(--primary-color);
}

.card-description {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin: 0;
}

.button-row {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
}

.button-row button {
    flex: 1;
    min-width: 140px;
}

.profile-list {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    max-height: 320px;
    overflow-y: auto;
}

.profile-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.5rem 0.6rem;
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.04);
    font-size: 0.85rem;
    flex-wrap: wrap;
}

.profile-info {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 160px;
}

.profile-info small {
    color: var(--text-muted);
    opacity: 0.8;
}

.profile-actions {
    flex: none;
}

.profile-actions button {
    flex: none;
    min-width: auto;
    padding: 0.25rem 0.75rem;
}
</style>
