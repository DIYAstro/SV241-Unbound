<script setup>
import { useModalStore } from '../../stores/modal'

const modal = useModalStore()

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
                onConfirm: async () => {
                    try {
                        const response = await fetch('/api/v1/backup/restore', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify(configContent)
                        });
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
      
      <!-- Top Row: Backup & Firmware (larger cards) -->
      <div class="actions-grid-2x2">
          <div class="action-card glass-panel">
              <h4>Backup & Restore</h4>
              <p class="card-description">Download or restore device configuration.</p>
              <div class="button-row">
                  <button @click="backupConfig" class="btn-secondary">Download Backup</button>
                  <button @click="restoreConfig" class="btn-secondary">Restore Backup</button>
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

/* Modal styles */
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

.restore-modal {
    padding: 2rem;
    max-width: 450px;
    width: 90%;
    text-align: center;
}

.restore-modal h3 {
    margin-top: 0;
    color: var(--success-color, #4caf50);
}

.restore-modal p {
    color: var(--text-secondary);
    margin-bottom: 1.5rem;
}

.modal-actions {
    display: flex;
    gap: 1rem;
    justify-content: center;
}

.modal-actions button {
    min-width: 120px;
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

