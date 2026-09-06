<script setup>
import { useModalStore } from '../../stores/modal'
import { useDeviceStore } from '../../stores/device'
import { storeToRefs } from 'pinia'
import { ref, watch } from 'vue'

const modal = useModalStore()
const store = useDeviceStore()
const { activeDeviceSerial, activeRigName, proxyConfig } = storeToRefs(store)

// Local edit buffer, separate from the store's live value so typing doesn't fight the 2s
// settings poll (checkConnection) overwriting the field mid-edit - same pattern as
// SwitchConfig.vue's `edits` ref.
const rigNameEdit = ref(activeRigName.value)
watch(activeRigName, (val) => { rigNameEdit.value = val })

async function saveRigName() {
    try {
        await store.saveProxyConfig({ ...store.proxyConfig, active_rig_name: rigNameEdit.value })
        modal.success('Rig name saved.')
    } catch (e) {
        modal.error('Error saving rig name: ' + e.message)
    }
}

// --- Automatic Backups ---
// Same local-edit-buffer pattern as rigNameEdit above.
const autoBackupEnabled = ref(true)
const autoBackupRetentionCount = ref(50)
watch(proxyConfig, (val) => {
    if (!val) return
    autoBackupEnabled.value = val.enableAutoBackup ?? true
    autoBackupRetentionCount.value = val.autoBackupRetentionCount ?? 50
}, { immediate: true })

async function saveAutoBackupSettings() {
    try {
        await store.saveProxyConfig({
            ...store.proxyConfig,
            enableAutoBackup: autoBackupEnabled.value,
            autoBackupRetentionCount: parseInt(autoBackupRetentionCount.value) || 0
        })
        modal.success('Automatic backup settings saved.')
    } catch (e) {
        modal.error('Error saving automatic backup settings: ' + e.message)
    }
}

const showRestoreModal = ref(false)
const autoBackups = ref([])
const loadingAutoBackups = ref(false)

// Opens the restore picker modal and refreshes the automatic-backup list at the same time -
// fetched lazily here rather than on component mount, so a page load that never opens Restore
// never makes the API call at all.
function openRestoreModal() {
    showRestoreModal.value = true
    fetchAutoBackups()
}

async function fetchAutoBackups() {
    loadingAutoBackups.value = true
    try {
        const response = await fetch('/api/v1/backup/list')
        if (response.ok) {
            autoBackups.value = await response.json()
        }
    } catch (e) {
        console.error('Failed to fetch automatic backups', e)
    } finally {
        loadingAutoBackups.value = false
    }
}

function formatBackupTimestamp(iso) {
    if (!iso) return 'Unknown time'
    return new Date(iso).toLocaleString()
}

// Mirrors performRestore() below - same device-mismatch confirmation flow, but restoring a file
// the server already has on disk (identified by filename) instead of one uploaded from the
// browser.
async function performAutoRestore(filename, force = false) {
    modal.loading('Please wait while the configuration is restored to the device…', 'Restoring Configuration')
    try {
        const url = `/api/v1/backup/restore-auto?file=${encodeURIComponent(filename)}${force ? '&force=true' : ''}`
        const response = await fetch(url, { method: 'POST' })

        if (response.status === 409) {
            const mismatch = await response.json()
            const backupLabel = mismatch.backupRigName || mismatch.backupDeviceSerial || 'an unknown box'
            const currentLabel = mismatch.currentRigName || mismatch.currentDeviceSerial || 'the connected box'
            modal.confirm(
                `This backup was taken from "${backupLabel}", but "${currentLabel}" is currently connected. ` +
                `Restoring it will overwrite the connected box's on-device settings (calibration, heater configuration, ` +
                `power startup states, etc.) with the ones from the backup. Continue anyway?`,
                {
                    title: 'Different Box Detected',
                    confirmText: 'Restore Anyway',
                    cancelText: 'Cancel',
                    onConfirm: () => performAutoRestore(filename, true)
                }
            );
            return;
        }

        if (!response.ok) throw new Error(response.statusText);

        modal.show({
            icon: '✅',
            title: 'Restore Successful',
            message: 'Configuration restored successfully! Would you like to reboot the device to apply all settings?',
            buttons: [
                {
                    text: 'Reboot Now',
                    action: async () => {
                        modal.close();
                        await fetch('/api/v1/command', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ command: 'reboot' })
                        });
                        setTimeout(() => location.reload(), 5000);
                    },
                    primary: true
                },
                {
                    text: 'Skip',
                    action: () => { modal.close(); location.reload(); }
                }
            ]
        });
    } catch (err) {
        modal.error('Restore failed: ' + err.message);
    }
}

function confirmAutoRestore(entry) {
    modal.confirm('This will overwrite your current configuration with this automatic backup. Continue?', {
        title: 'Restore Configuration',
        confirmText: 'Restore',
        cancelText: 'Cancel',
        onConfirm: () => {
            showRestoreModal.value = false
            performAutoRestore(entry.filename)
        }
    })
}

async function sendRebootCommand() {
    modal.confirm('Are you sure you want to reboot the device?', {
        title: 'Confirm Reboot',
        confirmText: 'Reboot',
        cancelText: 'Cancel',
        onConfirm: async () => {
            try {
                const response = await fetch('/api/v1/command', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ command: 'reboot' })
                });
                if (!response.ok) throw new Error(response.statusText);
                modal.success('Device is rebooting...', 'Reboot Initiated');
                setTimeout(() => location.reload(), 5000);
            } catch (e) {
                modal.error('Failed to reboot: ' + e.message);
            }
        }
    });
}

async function backupConfig() {
    try {
        const response = await fetch('/api/v1/backup/create');
        if (!response.ok) throw new Error('Backup failed');
        
        const blob = await response.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
        a.download = `sv241-backup-${timestamp}.json`;
        a.click();
        URL.revokeObjectURL(url);
        modal.success('Backup downloaded successfully.', 'Backup Complete');
    } catch (e) {
        modal.error('Backup failed: ' + e.message);
    }
}

function restoreConfig() {
    document.getElementById('restore-input').click();
}

// Sends the actual restore request. force=true skips the backend's device-mismatch check - used
// on retry once the user has explicitly confirmed the "different box detected" dialog below, e.g.
// because they're deliberately replacing one box with another.
async function performRestore(configContent, force = false) {
    modal.loading('Please wait while the configuration is restored to the device…', 'Restoring Configuration');
    try {
        const url = force ? '/api/v1/backup/restore?force=true' : '/api/v1/backup/restore';
        const response = await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(configContent)
        });

        if (response.status === 409) {
            const mismatch = await response.json();
            const backupLabel = mismatch.backupRigName || mismatch.backupDeviceSerial || 'an unknown box';
            const currentLabel = mismatch.currentRigName || mismatch.currentDeviceSerial || 'the connected box';
            modal.confirm(
                `This backup was taken from "${backupLabel}", but "${currentLabel}" is currently connected. ` +
                `Restoring it will overwrite the connected box's on-device settings (calibration, heater configuration, ` +
                `power startup states, etc.) with the ones from the backup. Continue anyway?`,
                {
                    title: 'Different Box Detected',
                    confirmText: 'Restore Anyway',
                    cancelText: 'Cancel',
                    onConfirm: () => performRestore(configContent, true)
                }
            );
            return;
        }

        if (!response.ok) throw new Error(response.statusText);

        // Show success modal with reboot option
        modal.show({
            icon: '✅',
            title: 'Restore Successful',
            message: 'Configuration restored successfully! Would you like to reboot the device to apply all settings?',
            buttons: [
                {
                    text: 'Reboot Now',
                    action: async () => {
                        modal.close();
                        await fetch('/api/v1/command', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ command: 'reboot' })
                        });
                        setTimeout(() => location.reload(), 5000);
                    },
                    primary: true
                },
                {
                    text: 'Skip',
                    action: () => { modal.close(); location.reload(); }
                }
            ]
        });
    } catch (err) {
        modal.error('Restore failed: ' + err.message);
    }
}

async function onFileSelected(event) {
    const file = event.target.files[0];
    if (!file) return;

    event.target.value = '';

    const reader = new FileReader();
    reader.onload = async (e) => {
        try {
            const configContent = JSON.parse(e.target.result);

            modal.confirm('This will overwrite your current configuration with the backup. Continue?', {
                title: 'Restore Configuration',
                confirmText: 'Restore',
                cancelText: 'Cancel',
                onConfirm: () => {
                    showRestoreModal.value = false;
                    performRestore(configContent);
                }
            });
        } catch (e) {
            modal.error('Invalid backup file: ' + e.message);
        }
    };
    reader.readAsText(file);
}

function handleFactoryReset() {
    modal.confirm('This will erase ALL device settings and restore factory defaults. This cannot be undone!', {
        title: 'Factory Reset',
        confirmText: 'Reset',
        cancelText: 'Cancel',
        onConfirm: async () => {
            try {
                const response = await fetch('/api/v1/command', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ command: 'factory_reset' })
                });
                if (!response.ok) throw new Error(response.statusText);
                
                // Show success modal with reboot option
                modal.show({
                    icon: '✅',
                    title: 'Factory Reset Successful',
                    message: 'Device has been reset to factory defaults. Would you like to reboot now to ensure all settings are applied?',
                    buttons: [
                        { 
                            text: 'Reboot Now', 
                            action: async () => {
                                modal.close();
                                await fetch('/api/v1/command', {
                                    method: 'POST',
                                    headers: { 'Content-Type': 'application/json' },
                                    body: JSON.stringify({ command: 'reboot' })
                                });
                                setTimeout(() => location.reload(), 5000);
                            }, 
                            primary: true 
                        },
                        { 
                            text: 'Skip', 
                            action: () => { modal.close(); location.reload(); }
                        }
                    ]
                });
            } catch (e) {
                modal.error('Factory reset failed: ' + e.message);
            }
        }
    });
}

function openFlasher() {
    window.location.href = '/flasher';
}
</script>

<template>
  <div class="config-group full-width-group">
      <h3>System Maintenance</h3>

      <!-- Device Identity: lets a user tell multiple boxes apart by a friendly label instead of
           the raw MAC. Only shown once a device has actually connected (activeDeviceSerial set) -
           there's nothing to name before that. -->
      <div v-if="activeDeviceSerial" class="action-card glass-panel" style="margin-bottom: 1rem;">
          <h4>Connected Box</h4>
          <p class="card-description">
              A friendly name for this specific SV241 box. Switch names, the Lens Temp label, and
              heater/weather preferences all travel with this name if you use more than one box.
          </p>
          <div class="button-row">
              <input type="text" v-model="rigNameEdit" placeholder="e.g. Imaging Rig" style="flex: 2;">
              <button @click="saveRigName" class="btn-secondary">Save</button>
          </div>
          <small style="color: var(--text-muted); opacity: 0.8;" :title="activeDeviceSerial">
              Serial: {{ activeDeviceSerial }}
          </small>
      </div>

      <!-- Top Row: Backup & Firmware (larger cards) -->
      <div class="actions-grid-2x2">
          <div class="action-card glass-panel">
              <h4>Backup & Restore</h4>
              <p class="card-description">Download or restore device configuration.</p>
              <div class="button-row">
                  <button @click="backupConfig" class="btn-secondary">Download Backup</button>
                  <button @click="openRestoreModal" class="btn-secondary">Restore Backup</button>
              </div>
              <input type="file" id="restore-input" style="display: none" accept=".json" @change="onFileSelected">
          </div>

          <div class="action-card glass-panel">
              <h4>Firmware Update</h4>
              <p class="card-description">Flash new firmware to the ESP32 device.</p>
              <div class="button-row">
                  <button @click="openFlasher" class="btn-secondary">Open Flasher Tool</button>
              </div>
          </div>
      </div>

      <!-- Automatic Backups: a full config+firmware snapshot written to disk every time the box
           connects (plus at least once every 24h while it stays connected), so there's always a
           recent backup without anyone having to remember to click "Download Backup". Just the
           settings live here - the list itself and its restore buttons live in the Restore modal
           above (opened via "Restore Backup"), so this card doesn't grow with every new backup. -->
      <div class="action-card glass-panel" style="margin-bottom: 1rem;">
          <h4>Automatic Backups</h4>
          <p class="card-description">
              Automatically backs up the full configuration (proxy + firmware) every time the box
              connects, and at least once every 24 hours while it stays connected.
          </p>
          <div class="button-row auto-backup-settings-row">
              <label class="checkbox-label">
                  <input type="checkbox" v-model="autoBackupEnabled">
                  Enabled
              </label>
              <label class="checkbox-label">
                  Keep last
                  <input type="number" v-model.number="autoBackupRetentionCount" min="0" style="width: 70px;">
                  (0 = unlimited)
              </label>
              <button @click="saveAutoBackupSettings" class="btn-secondary">Save</button>
          </div>
      </div>

      <!-- Restore picker modal: opened from the "Restore Backup" button above. Manual file
           restore first (reachable without scrolling even once the automatic-backup list below
           grows long), then the list of automatic backups in its own scroll area. -->
      <div v-if="showRestoreModal" class="modal-overlay" @click.self="showRestoreModal = false">
          <div class="modal-content restore-picker-modal">
              <h3>Restore Configuration</h3>

              <p class="card-description">Restore from a manually saved backup file:</p>
              <div class="button-row">
                  <button @click="restoreConfig" class="btn-secondary">Choose File…</button>
              </div>

              <hr class="divider">

              <p class="card-description">Or restore one of the automatic backups:</p>
              <div v-if="autoBackups.length" class="auto-backup-list">
                  <div v-for="entry in autoBackups" :key="entry.filename" class="auto-backup-row">
                      <span>
                          {{ formatBackupTimestamp(entry.timestamp) }}<template v-if="entry.rigName"> – {{ entry.rigName }}</template>
                      </span>
                      <button @click="confirmAutoRestore(entry)" class="btn-secondary">Restore</button>
                  </div>
              </div>
              <p v-else class="card-description">No automatic backups yet.</p>
              <div class="button-row">
                  <button @click="fetchAutoBackups" class="btn-secondary" :disabled="loadingAutoBackups">
                      {{ loadingAutoBackups ? 'Loading…' : 'Refresh List' }}
                  </button>
              </div>

              <div class="button-row" style="margin-top: 1rem;">
                  <button @click="showRestoreModal = false" class="btn-secondary">Close</button>
              </div>
          </div>
      </div>

      <!-- Bottom Row: Reboot & Reset (compact cards) -->
      <div class="actions-grid-2x2 compact-row">
          <div class="action-card-compact glass-panel">
              <h4>Power & Reboot</h4>
              <button @click="sendRebootCommand" class="btn-danger">Reboot Device</button>
          </div>

          <div class="action-card-compact glass-panel">
              <h4>Factory Reset</h4>
              <button @click="handleFactoryReset" class="btn-danger">Factory Reset</button>
          </div>
      </div>
  </div>
</template>

<style scoped>
.actions-grid-2x2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
    margin-bottom: 1rem;
}

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

.auto-backup-settings-row {
    align-items: center;
}

.checkbox-label {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    cursor: pointer;
    color: var(--text-secondary, #aaa);
    font-size: 0.85rem;
    flex: none;
    min-width: 0;
}

.checkbox-label input[type="number"] {
    width: 70px;
}

.auto-backup-list {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    max-height: 220px;
    overflow-y: auto;
}

.auto-backup-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.04);
    font-size: 0.85rem;
}

.auto-backup-row button {
    flex: none;
    min-width: auto;
    padding: 0.25rem 0.75rem;
}

/* Compact cards for bottom row */
.compact-row {
    margin-bottom: 0;
}

.action-card-compact {
    padding: 1rem;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
}

.action-card-compact h4 {
    margin: 0;
    color: var(--primary-color);
    font-size: 0.95rem;
}

.action-card-compact button {
    flex-shrink: 0;
}

/* Restore picker modal - deliberately does NOT redefine .modal-overlay/.modal-content, the
   global versions (assets/css/style.css) already give the right backdrop/z-index/theming, same
   pattern as OnboardingWizard.vue's .onboarding-modal. A previous, unrelated local override of
   .modal-overlay used to live here (higher specificity than the global rule due to Vue's scoped
   data-attribute, so it would have silently fought the global styling) - removed along with the
   dead .restore-modal/.modal-actions rules nothing in this file's template used anymore. */
.restore-picker-modal {
    max-width: 480px;
    width: 90%;
    text-align: left;
}

.restore-picker-modal h3 {
    margin: 0 0 1rem 0;
    color: var(--primary-color);
    text-align: center;
}

.divider {
    border: none;
    border-top: 1px solid rgba(255, 255, 255, 0.1);
    margin: 1rem 0;
}

/* Responsive: Stack on small screens */
@media (max-width: 600px) {
    .actions-grid-2x2 {
        grid-template-columns: 1fr;
    }
    .action-card-compact {
        flex-direction: column;
        align-items: stretch;
        text-align: center;
    }
}
</style>

