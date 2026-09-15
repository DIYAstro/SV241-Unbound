<script setup>
// Native Vue port of the (now removed) standalone frontend-vue/public/flasher/index.html page -
// same backend endpoints (internal/flasher's /api/v1/flash/{info,start,status} and /ws/flash),
// same version/erase decision logic, but rendered as a modal over the current Setup page instead
// of a full navigation away from it. Opened via stores/flasher.js's useFlasherStore from three
// places: SystemSettings.vue, OnboardingWizard.vue, UpdateBanner.vue.
import { ref, computed, onUnmounted, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useFlasherStore } from '../stores/flasher'
import { useModalStore } from '../stores/modal'
import { useDeviceStore } from '../stores/device'

const flasherStore = useFlasherStore()
const { isOpen } = storeToRefs(flasherStore)
const modal = useModalStore()
const deviceStore = useDeviceStore()

// view drives which part of the template is shown - 'progress' is deliberately the only phase
// that shows nothing but the progress bar/status text (no version info, checkbox, or buttons),
// per the actual request this component was built for.
const view = ref('loading') // loading | form | progress | done | error

const installedVersion = ref('unknown')
const bundledVersion = ref('unknown')
const upToDate = ref(false)
const forceErase = ref(false)
const eraseChecked = ref(false)
const defaultPort = ref('')
const portCandidates = ref([])
const selectedPort = ref('')
const backingUp = ref(false)

const percent = ref(0)
const phaseLabel = ref('')
const errorMessage = ref('')

const installedKnown = computed(() => installedVersion.value.toLowerCase() !== 'unknown')
const bundledKnown = computed(() => bundledVersion.value.toLowerCase() !== 'unknown')
const versionClass = computed(() => {
    if (upToDate.value) return 'match'
    if (installedKnown.value && bundledKnown.value) return 'mismatch'
    return ''
})

let ws = null
let pollTimer = null

// Opening/closing is driven entirely by the shared store (see its own doc comment for why) -
// this component is mounted once, unconditionally, in App.vue, exactly like OnboardingWizard.vue.
watch(isOpen, (open) => {
    if (open) {
        openModal()
    } else {
        cleanupConnections()
    }
})
onUnmounted(cleanupConnections)

async function openModal() {
    view.value = 'loading'

    // A flash job may already be running - e.g. started from another browser tab, or the user
    // reopening this modal after navigating away mid-flash. Jump straight to the progress view
    // in that case instead of showing the form again.
    try {
        const res = await fetch('/api/v1/flash/status')
        if (res.ok) {
            const status = await res.json()
            if (status.phase && !['idle', 'done', 'error'].includes(status.phase)) {
                enterProgressView()
                handleStatus(status)
                connectProgressSocket()
                return
            }
        }
    } catch (e) {
        // Fall through to the normal form fetch below.
    }

    await fetchInfo()
}

async function fetchInfo() {
    view.value = 'loading'
    try {
        const res = await fetch('/api/v1/flash/info')
        if (!res.ok) throw new Error('request failed')
        const info = await res.json()

        installedVersion.value = info.installedVersion || 'unknown'
        bundledVersion.value = info.bundledVersion || 'unknown'
        upToDate.value = !!info.upToDate
        forceErase.value = !!info.forceErase
        defaultPort.value = info.defaultPort || ''
        eraseChecked.value = !!info.forceErase || !!info.recommendEraseDefault
        portCandidates.value = []
        selectedPort.value = ''
    } catch (e) {
        // /api/v1/flash/info itself never fails in practice (it degrades to "unknown" internally
        // for anything it can't determine) - this only triggers if the proxy is unreachable
        // entirely, which is a real, rare case worth surfacing plainly.
        installedVersion.value = 'unknown'
        bundledVersion.value = 'unknown'
    }
    view.value = 'form'
}

function confirmAndStart() {
    const port = portCandidates.value.length ? selectedPort.value : ''
    const firmwareLabel = bundledKnown.value ? `firmware ${bundledVersion.value}` : 'the bundled firmware'
    const target = port || 'the SV241 box'
    const message = eraseChecked.value
        ? `This will flash ${target} with ${firmwareLabel} and erase all settings.`
        : `This will flash ${target} with ${firmwareLabel}.`

    modal.confirm(message, {
        title: 'Flash Firmware',
        confirmText: 'Flash',
        cancelText: 'Cancel',
        onConfirm: () => startFlash(port),
    })
}

async function startFlash(port) {
    try {
        const res = await fetch('/api/v1/flash/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ erase: eraseChecked.value, port }),
        })
        const data = await res.json().catch(() => ({}))

        if (res.status === 409 && data.error === 'ambiguous_port') {
            portCandidates.value = data.candidates || []
            selectedPort.value = portCandidates.value.includes(defaultPort.value)
                ? defaultPort.value
                : (portCandidates.value[0] || '')
            modal.info('Multiple candidate devices were found - pick the one to flash below and try again.', 'Pick a Device')
            return
        }
        if (!res.ok || !data.success) {
            throw new Error(data.error || 'Failed to start flash')
        }

        enterProgressView()
        connectProgressSocket()
    } catch (e) {
        modal.error('Could not start flashing: ' + e.message)
    }
}

function enterProgressView() {
    view.value = 'progress'
    percent.value = 0
    phaseLabel.value = 'Connecting to bootloader...'
}

// connectProgressSocket/startPolling mirror LiveLog.vue's WebSocket idiom against /ws/logs - here
// against /ws/flash (internal/flasher/ws.go). Unlike LiveLog.vue, polling isn't only a fallback
// for when the WS never connects at all - it runs continuously alongside the WS for as long as a
// flash is in progress, as a correctness backstop: /api/v1/flash/status always reflects the
// server's authoritative CurrentStatus() directly, so even if the WS silently missed a message
// (a slow client's queued update evicted under load - see ws.go's broadcast() - or any other
// transient hiccup that doesn't immediately fire onclose), the next poll tick catches up within
// 1.5s regardless. handleStatus() is idempotent, so the two sources overlapping is harmless. No
// reconnect-forever loop is needed the way LiveLog.vue has one: the flash job itself keeps
// running server-side regardless of this connection.
function connectProgressSocket() {
    startPolling()

    try {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        ws = new WebSocket(`${proto}//${window.location.host}/ws/flash`)
    } catch (e) {
        return // polling above already covers this
    }

    ws.onmessage = (event) => {
        let parsed
        try {
            parsed = JSON.parse(event.data)
        } catch (e) {
            return // malformed frame - ignore, the next message (or a poll tick) will catch up
        }
        try {
            handleStatus(parsed)
        } catch (e) {
            // Never let a bug in handleStatus (or anything it calls) get silently lost - it would
            // otherwise leave the UI stuck showing the last-good percent/message forever with no
            // visible sign anything went wrong (this is exactly how a previous real bug here -
            // deviceStore.fetchFirmwareVersion not being exported from the store - went unnoticed
            // until logged explicitly).
            console.error('FirmwareFlasher: handleStatus failed', e)
        }
    }
    ws.onerror = () => { ws?.close() }
    ws.onclose = () => { ws = null }
}

function startPolling() {
    if (pollTimer) return
    pollTimer = setInterval(async () => {
        try {
            const res = await fetch('/api/v1/flash/status')
            const data = await res.json()
            try {
                handleStatus(data)
            } catch (e) {
                console.error('FirmwareFlasher: handleStatus failed', e)
            }
        } catch (e) { /* transient fetch failure - try again next tick */ }
    }, 1500)
}

function stopPolling() {
    if (pollTimer) {
        clearInterval(pollTimer)
        pollTimer = null
    }
}

function closeSocket() {
    if (ws) {
        ws.close()
        ws = null
    }
}

function cleanupConnections() {
    stopPolling()
    closeSocket()
}

const phaseLabels = {
    idle: 'Ready.',
    connecting: 'Connecting to bootloader...',
    erasing: 'Erasing flash...',
    writing: 'Writing firmware...',
    verifying: 'Verifying...',
    resetting: 'Resetting device...',
}

function handleStatus(s) {
    percent.value = Math.max(0, Math.min(100, s.percent || 0))
    phaseLabel.value = s.message || phaseLabels[s.phase] || '...'

    if (s.phase === 'done') {
        cleanupConnections()
        percent.value = 100
        deviceStore.fetchFirmwareVersion()
        view.value = 'done'
    } else if (s.phase === 'error') {
        cleanupConnections()
        errorMessage.value = s.error || 'Unknown error'
        view.value = 'error'
    }
}

// Closing by clicking the overlay background is disabled while a flash is actively running
// (view === 'progress') - the job would keep going server-side regardless, but abandoning the
// only view of it here invites confusion, not real risk. See the 'done'/'error' views' own
// explicit close buttons for the normal way out.
function handleOverlayClick() {
    if (view.value !== 'progress') {
        flasherStore.close()
    }
}

async function backupConfig() {
    backingUp.value = true
    try {
        const response = await fetch('/api/v1/backup/create')
        if (!response.ok) throw new Error('Backup failed')

        const blob = await response.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
        a.download = `sv241-backup-${timestamp}.json`
        a.click()
        URL.revokeObjectURL(url)
        modal.success('Backup downloaded successfully.', 'Backup Complete')
    } catch (e) {
        modal.error('Backup failed: ' + e.message)
    } finally {
        backingUp.value = false
    }
}
</script>

<template>
  <div v-if="isOpen" class="modal-overlay" @click.self="handleOverlayClick">
    <div class="modal-content flasher-modal">
      <h3>Firmware Update</h3>

      <p v-if="view === 'loading'" class="flasher-text">Loading…</p>

      <template v-else-if="view === 'form'">
        <div class="version-info">
          <div class="version-item">
            <div class="version-label">Installed Firmware</div>
            <div class="version-value" :class="versionClass">{{ installedVersion }}</div>
          </div>
          <div class="version-item">
            <div class="version-label">Bundled Firmware</div>
            <div class="version-value" :class="versionClass">{{ bundledVersion }}</div>
          </div>
        </div>

        <div v-if="upToDate" class="flasher-box flasher-info">
          <i class="ri-checkbox-circle-line"></i> Firmware is up to date. No update required.
        </div>
        <div v-else-if="installedKnown && !bundledKnown" class="flasher-box flasher-info">
          <i class="ri-information-line"></i> Bundled firmware version unknown - unable to compare. You can still flash below.
        </div>
        <div v-else-if="installedKnown && forceErase" class="flasher-box flasher-warning">
          <i class="ri-error-warning-line"></i> Your installed firmware ({{ installedVersion }}) predates 0.9.15, which changed how
          settings are stored on the device. This update will reset your settings to defaults -
          that's expected for this one-time transition. Future updates will preserve your settings
          normally.
        </div>
        <div v-else-if="installedKnown" class="flasher-box flasher-warning">
          <i class="ri-error-warning-line"></i> Firmware version mismatch. Consider updating to ensure compatibility.
        </div>

        <p class="flasher-text">
          Firmware updates preserve your existing settings (switch names, startup states, heater
          config, etc.) by default.
        </p>
        <div class="button-row">
          <button @click="backupConfig" class="btn-secondary flasher-full-width" :disabled="backingUp">
            <i class="ri-save-line"></i> {{ backingUp ? 'Creating backup…' : 'Backup Now' }}
          </button>
        </div>

        <label class="flasher-checkbox-label">
          <input type="checkbox" v-model="eraseChecked" :disabled="forceErase">
          Erase device and reset all settings to defaults
        </label>

        <div class="flasher-box flasher-warning">
          <i class="ri-error-warning-line"></i> Verify Device: Make absolutely sure you select the correct device below! If you have
          other ESP32 devices connected, their firmware could be overwritten instead.
        </div>

        <div v-if="portCandidates.length" class="flasher-port-picker">
          <label for="flasher-port-select">Device to flash</label>
          <select id="flasher-port-select" v-model="selectedPort">
            <option v-for="name in portCandidates" :key="name" :value="name">{{ name }}</option>
          </select>
        </div>

        <div class="button-row flasher-actions">
          <button @click="flasherStore.close()" class="btn-secondary">Cancel</button>
          <button @click="confirmAndStart" class="btn-primary"><i class="ri-flashlight-line"></i> Start Flashing</button>
        </div>
      </template>

      <template v-else-if="view === 'progress'">
        <div class="flasher-progress-track">
          <div class="flasher-progress-fill" :style="{ width: percent + '%' }"></div>
        </div>
        <p class="flasher-text flasher-status">{{ phaseLabel }}</p>
      </template>

      <template v-else-if="view === 'done'">
        <div class="flasher-box flasher-info"><i class="ri-checkbox-circle-line"></i> Flash complete! Firmware updated successfully.</div>
        <div class="button-row flasher-actions">
          <button @click="flasherStore.close()" class="btn-primary">Back to Setup</button>
        </div>
      </template>

      <template v-else-if="view === 'error'">
        <div class="flasher-box flasher-warning"><i class="ri-close-circle-line"></i> Flash failed: {{ errorMessage }}</div>
        <div class="button-row flasher-actions">
          <button @click="fetchInfo" class="btn-secondary">Try Again</button>
          <button @click="flasherStore.close()" class="btn-primary">Back to Setup</button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
/* Deliberately does NOT redefine .modal-overlay/.modal-content - the global versions
   (assets/css/style.css) already give the right backdrop/z-index/theming, same pattern as
   OnboardingWizard.vue's .onboarding-modal and SystemSettings.vue's .restore-picker-modal. */
.flasher-modal {
    max-width: 480px;
    width: 90%;
    text-align: center;
}

.flasher-modal h3 {
    margin-top: 0;
    color: var(--primary-color);
}

.flasher-text {
    color: var(--text-secondary);
    font-size: 0.9rem;
    text-align: left;
    line-height: 1.5;
}

.version-info {
    display: flex;
    justify-content: space-around;
    margin: 1rem 0;
    padding: 1rem;
    background: rgba(0, 0, 0, 0.2);
    border-radius: var(--radius-sm);
    border: 1px solid var(--surface-border);
}

.version-item {
    text-align: center;
}

.version-label {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-bottom: 0.25rem;
}

.version-value {
    font-size: 1.1rem;
    font-weight: 500;
    color: var(--text-primary);
}

.version-value.match {
    color: var(--success-color);
}

.version-value.mismatch {
    color: var(--warning-color);
}

.flasher-box {
    padding: 0.85rem 1rem;
    border-radius: var(--radius-sm);
    margin: 1rem 0;
    font-size: 0.85rem;
    text-align: left;
    line-height: 1.4;
}

.flasher-info {
    background: var(--primary-glow);
    border: 1px solid var(--primary-color);
    color: var(--text-primary);
}

.flasher-warning {
    background: rgba(255, 183, 77, 0.12);
    border: 1px solid var(--warning-color);
    color: var(--warning-color);
}

.flasher-checkbox-label {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    text-align: left;
    font-size: 0.85rem;
    color: var(--text-secondary);
    margin: 1rem 0;
    cursor: pointer;
}

.flasher-checkbox-label input {
    margin-top: 0.2rem;
}

.flasher-port-picker {
    text-align: left;
    margin: 1rem 0;
}

.flasher-port-picker label {
    display: block;
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-bottom: 0.4rem;
}

.flasher-port-picker select {
    width: 100%;
    padding: 0.6rem;
    border-radius: var(--radius-sm);
    border: 1px solid var(--surface-border);
    background: rgba(0, 0, 0, 0.3);
    color: var(--text-primary);
    font-family: var(--font-main);
    font-size: 0.9rem;
}

.flasher-actions {
    margin-top: 1.25rem;
}

.flasher-actions button {
    flex: 1;
}

.flasher-full-width {
    width: 100%;
}

.flasher-progress-track {
    width: 100%;
    height: 10px;
    border-radius: 5px;
    background: rgba(0, 0, 0, 0.3);
    overflow: hidden;
    margin: 1.5rem 0 0.75rem;
}

.flasher-progress-fill {
    height: 100%;
    width: 0%;
    background: var(--primary-color);
    transition: width 0.3s ease;
}

.flasher-status {
    text-align: center;
    font-style: italic;
}
</style>
