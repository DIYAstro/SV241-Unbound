import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useDeviceStore = defineStore('device', () => {
    const firmwareVersion = ref('-')
    const comPort = ref('-')
    const isConnected = ref(false)
    const connectionStatus = ref('Disconnected')
    const proxyVersion = ref('')
    const isPaused = ref(false)
    const liveStatus = ref({
        v: 0, i: 0, p: 0,
        t_amb: 0, h_amb: 0, d: 0,
        t_lens: 0,
        pwm1: 0, pwm2: 0
    })
    const telemetryHistory = ref([]) // For charts
    const availableDates = ref([]) // For CSV history selection
    const activeSwitches = ref({})
    const switchNames = ref({})
    const powerStatus = ref({})
    const config = ref({})
    const proxyConfig = ref({})

    // Actions
    async function fetchConfig() {
        try {
            const response = await fetch('/api/v1/config');
            if (response.ok) {
                config.value = await response.json();
            }
        } catch (e) {
            console.error("Failed to fetch config", e);
        }
    }

    async function fetchProxySettings() {
        try {
            const response = await fetch('/api/v1/settings');
            if (response.ok) {
                const data = await response.json();
                // The endpoint returns { proxy_config: {...}, ... } or just the config?
                // app.js: const settings = await response.json(); const proxyConf = settings.proxy_config;
                // But wait, check checkConnection(). It calls /api/v1/settings.
                // So we already fetch it there.
                // Let's reuse that or standardize.
            }
        } catch (e) {
            console.error("Failed to fetch proxy settings", e);
        }
    }

    async function saveProxyConfig(newSettings) {
        try {
            const response = await fetch('/api/v1/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newSettings)
            });
            if (!response.ok) throw new Error(response.statusText);

            // Refresh
            checkConnection();
            return true;
        } catch (e) {
            console.error("Failed to save proxy config", e);
            throw e;
        }
    }

    async function saveConfig(newConfigChunk) {
        // Merge with existing config for safety, though API likely handles partials? 
        // app.js sends e.g. { dh: [...] } or { ps: ... }
        try {
            const response = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfigChunk)
            });
            if (!response.ok) throw new Error(response.statusText);

            // Refresh config after save
            await fetchConfig();
            return true;
        } catch (e) {
            console.error("Failed to save config", e);
            throw e;
        }
    }

    async function fetchPowerStatus() {
        try {
            const response = await fetch('/api/v1/power/status');
            if (response.ok) {
                powerStatus.value = await response.json();
            }
        } catch (e) {
            // console.error("Failed to fetch power status", e);
        }
    }

    async function setSwitch(id, state) {
        const formData = new URLSearchParams();
        formData.append('Id', id);
        formData.append('State', state); // true/false or 1/0? Alpaca usually expects boolean or "True"/"False"?
        // app.js sends checked (boolean). Alpaca handler in Go usually parses bool or string.
        // app.js line 1225: formData.append('State', state); where state is boolean.
        // JS String(true) -> "true".
        try {
            await fetch('/api/v1/switch/0/setswitchvalue', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: formData
            });
            // Optimistic update or fetch
            setTimeout(fetchPowerStatus, 200);
        } catch (error) {
            console.error(`Error setting switch ${id}:`, error);
        }
    }

    async function setAllPower(state) {
        try {
            await fetch('/api/v1/power/all', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ state })
            });
            setTimeout(fetchPowerStatus, 500);
        } catch (error) {
            console.error('Error setting all power states:', error);
        }
    }

    async function fetchLiveStatus() {
        try {
            const response = await fetch('/api/v1/status');
            if (response.ok) {
                liveStatus.value = await response.json();
            }
        } catch (e) {
            console.error("Failed to fetch live status", e);
        }
    }

    async function fetchProxyVersion() {
        try {
            const response = await fetch('/api/v1/proxy/version')
            if (response.ok) {
                const data = await response.json()
                proxyVersion.value = data.version ? `(${data.version})` : ''
            }
        } catch (e) {
            console.error("Failed to fetch proxy version", e)
        }
    }

    async function fetchFirmwareVersion() {
        try {
            const response = await fetch('/api/v1/firmware/version')
            if (response.ok) {
                const data = await response.json()
                firmwareVersion.value = data.version || 'Unknown'
            }
        } catch (e) {
            console.error("Failed to fetch firmware version", e)
        }
    }

    async function checkConnection() {
        try {
            const response = await fetch('/api/v1/settings');
            if (!response.ok) {
                // If settings fail, we might be disconnected or proxy is down
                isConnected.value = false;
                connectionStatus.value = "Disconnected";
                comPort.value = "Proxy Offline";
                return;
            }

            const settings = await response.json();
            const proxyConf = settings.proxy_config;

            // Store full proxy config for settings page
            if (proxyConf) {
                proxyConfig.value = proxyConf;
            }

            if (settings.active_switches) {
                activeSwitches.value = settings.active_switches;
            }
            if (proxyConf && proxyConf.switchNames) {
                switchNames.value = proxyConf.switchNames;
            }

            isPaused.value = settings.reconnect_paused === true;

            if (proxyConf && proxyConf.serialPortName) {
                if (isPaused.value) {
                    isConnected.value = false;
                    connectionStatus.value = "Paused";
                    comPort.value = `${proxyConf.serialPortName} (Paused)`;
                } else if (settings.serial_port_connected) {
                    isConnected.value = true;
                    connectionStatus.value = "Connected";
                    comPort.value = proxyConf.serialPortName;
                } else {
                    isConnected.value = false;
                    connectionStatus.value = "Connecting...";
                    comPort.value = `Connecting to ${proxyConf.serialPortName}...`;
                }
            } else {
                isConnected.value = false;
                connectionStatus.value = "Auto-detecting...";
                comPort.value = "Auto-detecting...";
            }

        } catch (error) {
            console.error("Connection check failed", error);
            isConnected.value = false;
            connectionStatus.value = "Disconnected";
            comPort.value = "Proxy Offline";
        }
    }

    // Polling
    function startPolling() {
        fetchProxyVersion();
        fetchFirmwareVersion();
        checkConnection();

        setInterval(() => {
            checkConnection();
            // Firmware version usually doesn't change often, but we can poll it less frequently or same
            if (isConnected.value) {
                fetchFirmwareVersion();
                fetchLiveStatus();
                fetchPowerStatus();
                if (Object.keys(config.value).length === 0) fetchConfig();
            }
        }, 2000);
    }

    async function fetchDates() {
        try {
            const response = await fetch('/api/v1/telemetry/dates');
            if (response.ok) {
                availableDates.value = await response.json();
            }
        } catch (e) { console.error("Failed to fetch dates", e); }
    }

    async function fetchHistory(date = null) {
        try {
            let url = '/api/v1/telemetry/history';
            if (date) {
                url += `?date=${date}`;
            } else {
                url += `?duration=12h`; // Default view
            }

            const response = await fetch(url);
            if (response.ok) {
                let rawData = await response.json();
                if (!rawData) rawData = []; // Handle null/nil from Go
                // Map short JSON keys to store format
                // API returns: t(sec), v, c, p, temp, hum, dew, lens, pwm1, pwm2
                telemetryHistory.value = rawData.map(d => ({
                    timestamp: d.t * 1000, // Convert unix seconds to JS milliseconds
                    v: d.v,
                    i: d.c || 0, // 'c' in API -> 'i' in store
                    p: d.p,
                    t_amb: d.temp,
                    h_amb: d.hum,
                    d: d.dew,
                    t_lens: d.lens,
                    pwm1: d.pwm1,
                    pwm2: d.pwm2
                }));
            }
        } catch (e) {
            console.error("Failed to fetch history", e);
        }
    }

    function getDownloadUrl(date) {
        return `/api/v1/telemetry/download?date=${date}`;
    }

    return {
        firmwareVersion,
        comPort,
        isConnected,
        connectionStatus,
        proxyVersion,
        liveStatus,
        activeSwitches,
        switchNames,
        powerStatus,
        config,
        proxyConfig,
        telemetryHistory,
        availableDates,
        fetchConfig,
        saveConfig,
        fetchProxySettings,
        saveProxyConfig,
        setSwitch,
        setAllPower,
        startPolling: () => {
            startPolling();
            // Initial history fetch (default 12h)
            fetchHistory();
            fetchDates();
            // Poll history every 5 minutes
            setInterval(() => fetchHistory(), 60000 * 5);
        },
        checkConnection,
        fetchHistory,
        fetchDates,
        getDownloadUrl
    }
})
