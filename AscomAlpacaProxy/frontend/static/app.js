document.addEventListener('DOMContentLoaded', () => {
    // --- DOM Elements ---
    // --- DOM Elements ---
    const comPortElement = document.querySelector('#com-port-badge .value');
    const firmwareVersionElement = document.querySelector('#firmware-badge .value');
    const proxyVersionDisplay = document.getElementById('proxy-version-display');
    // const deviceResponseElement = document.getElementById('device-response'); // Removed
    const connectionIndicator = document.getElementById('connection-indicator');
    const connectionText = document.getElementById('connection-text');
    const masterPowerSwitch = document.getElementById('master-power-switch');
    const rebootButton = document.getElementById('reboot-button');
    const factoryResetButton = document.getElementById('factory-reset-button');
    const backupButton = document.getElementById('backup-button');
    const restoreButton = document.getElementById('restore-button');
    const restoreFileInput = document.getElementById('restore-file-input');
    // Firmware element is now defined above

    // --- State ---
    let originalConfig = {};
    let originalProxyConfig = {};
    let switchIDMap = {
        0: "dc1", 1: "dc2", 2: "dc3", 3: "dc4", 4: "dc5",
        5: "usbc12", 6: "usb345", 7: "adj_conv", 8: "pwm1", 9: "pwm2",
    };
    const switchNameMap = {
        "d1": "DC 1", "d2": "DC 2", "d3": "DC 3", "d4": "DC 4", "d5": "DC 5",
        "u12": "USB (C/1/2)", "u34": "USB (3/4/5)", "adj": "Adj. Voltage",
        "pwm1": "PWM 1", "pwm2": "PWM 2",
    };
    const longToShortKeyMap = {
        "dc1": "DC 1", "dc2": "DC 2", "dc3": "DC 3", "dc4": "DC 4", "dc5": "DC 5",
        "usbc12": "USB (C/1/2)", "usb345": "USB (3/4/5)", "adj_conv": "Adj. Voltage",
        "pwm1": "PWM 1", "pwm2": "PWM 2",
    };

    // --- Connection Status ---
    function updateConnectionStatus(proxyConf, statusOk) {
        if (!proxyConf || Object.keys(proxyConf).length === 0) {
            connectionIndicator.className = 'disconnected';
            if (connectionText) connectionText.textContent = "Disconnected";
            document.querySelectorAll('.save-config-button').forEach(btn => btn.disabled = true);
            comPortElement.textContent = "Proxy Offline";
            return;
        }

        if (proxyConf.serialPortName) {
            if (statusOk) {
                document.querySelectorAll('.save-config-button').forEach(btn => btn.disabled = false);
                connectionIndicator.className = 'connected';
                if (connectionText) connectionText.textContent = "Connected";
                comPortElement.textContent = proxyConf.serialPortName;
            } else {
                document.querySelectorAll('.save-config-button').forEach(btn => btn.disabled = true);
                connectionIndicator.className = 'disconnected';
                if (connectionText) connectionText.textContent = "Connecting...";
                comPortElement.textContent = `Connecting to ${proxyConf.serialPortName}...`;
            }
        } else {
            connectionIndicator.className = 'connecting';
            if (connectionText) connectionText.textContent = "Auto-detecting...";
            comPortElement.textContent = "Auto-detecting...";
        }

    }

    function logToViewer(message, type = 'INFO') {
        const logContainer = document.getElementById('log-container');
        if (!logContainer) return;

        const timestamp = new Date().toLocaleTimeString();
        const line = document.createElement('div');
        line.classList.add('log-line');
        if (type === 'ERROR') line.classList.add('log-line-error');
        if (type === 'WARN') line.classList.add('log-line-warning');
        if (type === 'DEBUG') line.classList.add('log-line-debug');

        line.innerHTML = `<span class="log-time">[${timestamp}]</span> <span class="log-msg">${message}</span>`;
        logContainer.appendChild(line);
        logContainer.scrollTop = logContainer.scrollHeight;
    }

    function showResponse(message, isError = false) {
        logToViewer(message, isError ? 'ERROR' : 'INFO');
    }

    async function fetchFirmwareVersion() {
        try {
            const response = await fetch('/api/v1/firmware/version');
            if (response.ok) {
                const data = await response.json();
                if (firmwareVersionElement) {
                    firmwareVersionElement.textContent = data.version || 'Unknown';
                }
            }
        } catch (e) {
            console.error("Failed to fetch firmware version", e);
        }
    }

    async function fetchProxyVersion() {
        try {
            const response = await fetch('/api/v1/proxy/version');
            if (response.ok) {
                const data = await response.json();
                if (proxyVersionDisplay && data.version) {
                    proxyVersionDisplay.textContent = `(${data.version})`;
                }
            }
        } catch (e) {
            console.error("Failed to fetch proxy version", e);
        }
    }

    // --- Unsaved Changes ---
    function setupChangeDetection() {
        const configForm = document.getElementById('config-section'); // Use config-section as container
        if (!configForm) return;

        configForm.addEventListener('input', (e) => {
            const changedSection = e.target.closest('.config-group') || e.target.closest('.collapsible-content') || e.target.closest('.tab-content');

            if (changedSection) {
                const saveButton = changedSection.querySelector('button.save-config-button') || changedSection.querySelector('button#save-switch-names-button') || changedSection.querySelector('button#save-proxy-config-button');
                const header = changedSection.querySelector('h3, h4');

                if (saveButton) {
                    saveButton.classList.add('needs-saving');
                }
                if (header) {
                    header.classList.add('has-unsaved-changes');
                }
            }
        });
    }

    function resetUnsavedIndicators() {
        document.querySelectorAll('button.needs-saving').forEach(btn => btn.classList.remove('needs-saving'));
        document.querySelectorAll('.has-unsaved-changes').forEach(header => header.classList.remove('has-unsaved-changes'));
    }

    // --- Heater Mode UI ---
    function updateHeaterModeView(heaterIndex) {
        const selectedMode = document.getElementById(`heater-${heaterIndex}-mode`).value;
        document.querySelectorAll(`#heater-${heaterIndex} .mode-settings`).forEach(el => el.classList.remove('active'));
        const activeSettings = document.getElementById(`heater-${heaterIndex}-mode-${selectedMode}`);
        if (activeSettings) activeSettings.classList.add('active');

        // Feature: Disable and clear custom name if Heater is disabled (Mode 5)
        const nameInput = document.getElementById(`heater-${heaterIndex}-custom-name`);
        if (nameInput) {
            if (selectedMode === "5") {
                nameInput.value = "";
                nameInput.disabled = true;
                // Update header immediately to reflect removal
                if (typeof updateHeaterHeader === 'function') {
                    updateHeaterHeader(heaterIndex, "");
                }
            } else {
                nameInput.disabled = false;
            }
        }
    }

    function updateHeaterOptions() {
        const heater0ModeSelect = document.getElementById('heater-0-mode');
        const heater1ModeSelect = document.getElementById('heater-1-mode');
        if (!heater0ModeSelect || !heater1ModeSelect) return;

        const exclusiveModes = ['1', '4']; // Values for 'PID (Lens Sensor)' and 'Minimum Temperature'
        const pidMode = '1';
        const syncMode = '3';

        // 1. Reset by enabling all options on both selects
        for (const option of heater0ModeSelect.options) { option.disabled = false; }
        for (const option of heater1ModeSelect.options) { option.disabled = false; }

        // Function to apply disabling logic
        const applyConstraints = () => {
            const val0 = heater0ModeSelect.value;
            const val1 = heater1ModeSelect.value;
            const disabledMode = '5';

            // 1.5 Handle Startup Enable Checkbox for Disabled Mode
            const startupCheckbox0 = document.getElementById('heater-0-startup-enabled');
            if (startupCheckbox0) {
                if (val0 === disabledMode) {
                    startupCheckbox0.checked = false;
                    startupCheckbox0.disabled = true;
                } else {
                    startupCheckbox0.disabled = false;
                }
            }

            const startupCheckbox1 = document.getElementById('heater-1-startup-enabled');
            if (startupCheckbox1) {
                if (val1 === disabledMode) {
                    startupCheckbox1.checked = false;
                    startupCheckbox1.disabled = true;
                } else {
                    startupCheckbox1.disabled = false;
                }
            }

            // 2. Handle PID-Sync (Follower) mode constraints
            const syncOption1 = heater1ModeSelect.querySelector(`option[value="${syncMode}"]`);
            if (syncOption1) syncOption1.disabled = (val0 !== pidMode);

            const syncOption0 = heater0ModeSelect.querySelector(`option[value="${syncMode}"]`);
            if (syncOption0) syncOption0.disabled = (val1 !== pidMode);

            // 3. Handle exclusive modes (PID and Minimum Temperature)
            if (exclusiveModes.includes(val0)) {
                for (const option of heater1ModeSelect.options) {
                    if (exclusiveModes.includes(option.value)) {
                        option.disabled = true;
                    }
                }
            }
            if (exclusiveModes.includes(val1)) {
                for (const option of heater0ModeSelect.options) {
                    if (exclusiveModes.includes(option.value)) {
                        option.disabled = true;
                    }
                }
            }
        };

        applyConstraints();

        // 4. Final check: If a current selection has become invalid, reset it to 'Manual'
        let resetOccurred = false;
        if (heater0ModeSelect.options[heater0ModeSelect.selectedIndex].disabled) {
            heater0ModeSelect.value = '0';
            updateHeaterModeView(0);
            resetOccurred = true;
        }
        if (heater1ModeSelect.options[heater1ModeSelect.selectedIndex].disabled) {
            heater1ModeSelect.value = '0';
            updateHeaterModeView(1);
            resetOccurred = true;
        }

        // 5. If a reset happened, re-apply constraints to ensure state is consistent
        if (resetOccurred) {
            applyConstraints();
        }
    }

    // --- Data Fetching and Population ---
    async function fetchConfig() {
        try {
            const response = await fetch('/api/v1/config');
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
            const config = await response.json();
            originalConfig = JSON.parse(JSON.stringify(config)); // Deep copy

            // document.getElementById('adj-voltage').value ... // Removed: now in table

            if (config.so) { // sensor_offsets -> so
                document.getElementById('offset-sht40-temp').value = config.so.st;
                document.getElementById('offset-sht40-humidity').value = config.so.sh;
                document.getElementById('offset-ds18b20-temp').value = config.so.dt;
                document.getElementById('offset-ina219-voltage').value = config.so.iv;
                document.getElementById('offset-ina219-current').value = config.so.ic;
            }

            if (config.ps) { // power_startup_states -> ps
                // Pass current known switch names (might be empty initially if proxy config not fetched yet)
                // We will also call this from fetchProxyConfig to ensure it updates when names arrive.
                populateSwitchConfigTable(window.currentSwitchNames, config.ps, config.av);
                populatePowerControls(window.currentSwitchNames); // Update grid to apply filters
            }

            if (config.ui) { // update_intervals_ms -> ui
                document.getElementById('interval-ina219').value = config.ui.i;
                document.getElementById('interval-sht40').value = config.ui.s;
                document.getElementById('interval-ds18b20').value = config.ui.d;
            }

            if (config.ac) { // averaging_counts -> ac
                document.getElementById('avg-sht40-temp').value = config.ac.st;
                document.getElementById('avg-sht40-humidity').value = config.ac.sh;
                document.getElementById('avg-ds18b20-temp').value = config.ac.dt;
                document.getElementById('avg-ina219-voltage').value = config.ac.iv;
                document.getElementById('avg-ina219-current').value = config.ac.ic;
            }

            if (config.dh) { // Überprüft, ob 'dh' existiert, bevor iteriert wird
                config.dh.forEach((heaterConf, i) => { // dew_heaters -> dh
                    // Verwendet den Nullish Coalescing Operator (??) für Standardwerte
                    document.getElementById(`heater-${i}-mode`).value = heaterConf.m ?? 0;
                    document.getElementById(`heater-${i}-startup-enabled`).checked = heaterConf.en ?? false;
                    document.getElementById(`heater-${i}-manual-power`).value = heaterConf.mp ?? 0; // Standard auf 0 für Leistungsprozentsatz
                    document.getElementById(`heater-${i}-target-offset`).value = heaterConf.to ?? '';
                    document.getElementById(`heater-${i}-pid-kp`).value = heaterConf.kp ?? '';
                    document.getElementById(`heater-${i}-pid-ki`).value = heaterConf.ki ?? '';
                    document.getElementById(`heater-${i}-pid-kd`).value = heaterConf.kd ?? '';
                    document.getElementById(`heater-${i}-start-delta`).value = heaterConf.sd ?? '';
                    document.getElementById(`heater-${i}-end-delta`).value = heaterConf.ed ?? '';
                    document.getElementById(`heater-${i}-max-power`).value = heaterConf.xp ?? 0; // Standard auf 0 für maximalen Leistungsprozentsatz
                    document.getElementById(`heater-${i}-pid-sync-factor`).value = heaterConf.psf ?? '';

                    // New fields for Minimum Temperature mode
                    document.getElementById(`heater-${i}-min-temp`).value = heaterConf.mt ?? '';
                    // Also populate the duplicated PID fields for mode 4
                    document.getElementById(`heater-${i}-target-offset-4`).value = heaterConf.to ?? '';
                    document.getElementById(`heater-${i}-pid-kp-4`).value = heaterConf.kp ?? '';
                    document.getElementById(`heater-${i}-pid-ki-4`).value = heaterConf.ki ?? '';
                    document.getElementById(`heater-${i}-pid-kd-4`).value = heaterConf.kd ?? '';

                    updateHeaterModeView(i);
                });
                updateHeaterOptions(); // Set initial constraints after loading
            }

            // Moved logToViewer and showResponse to global scope

            // --- State ---
            // ... (rest of state is defined in the previous block scope, but for this edit we are inside the DOMContentLoaded)

            // Helper to replace deviceResponseElement usage

            // ... existing loadConfig function ...
            if (config.ad) { // auto_dry -> ad
                document.getElementById('auto-dry-enabled').checked = config.ad.en === 1;
                document.getElementById('auto-dry-humidity-threshold').value = config.ad.ht ?? 95;
                document.getElementById('auto-dry-trigger-duration').value = config.ad.td ?? 180;
            }

            showResponse("Configuration loaded successfully.");
            return true;
        } catch (error) {
            console.error('Error fetching config:', error);
            showResponse(`Error fetching config: ${error.message}`, true);
            return false;
        }
    }

    function applyProxyConfig(proxyConf, availableIPs = []) {
        document.getElementById('proxy-serial-port').value = proxyConf.serialPortName || '';
        document.getElementById('proxy-auto-detect-port').checked = proxyConf.autoDetectPort;
        document.getElementById('proxy-enable-alpaca-voltage').checked = proxyConf.enableAlpacaVoltageControl;
        const mpState = (proxyConf.enableMasterPower !== false) ? 'enabled' : 'disabled';
        document.getElementById('proxy-master-power-state').value = mpState;
        document.getElementById('proxy-serial-port').disabled = proxyConf.autoDetectPort;
        document.getElementById('proxy-network-port').value = proxyConf.networkPort || 8080;
        document.getElementById('proxy-log-level').value = proxyConf.logLevel || 'INFO';
        document.getElementById('proxy-history-retention').value = proxyConf.historyRetentionNights || 30;

        // Populate Listen Address dropdown
        const listenAddressSelect = document.getElementById('proxy-listen-address');
        listenAddressSelect.innerHTML = ''; // Clear existing options

        // Add static options that should always be available
        const staticIPs = ['127.0.0.1', '0.0.0.0'];
        staticIPs.forEach(ip => {
            const option = document.createElement('option');
            option.value = ip;
            option.textContent = ip;
            if (ip === '127.0.0.1') option.textContent += ' (Local Only)';
            if (ip === '0.0.0.0') option.textContent += ' (All Interfaces)';
            listenAddressSelect.appendChild(option);
        });

        // Add dynamic IPs from server, avoiding duplicates
        if (availableIPs) {
            availableIPs.forEach(ip => {
                // Filter out link-local addresses (APIPA)
                if (ip.startsWith('169.254.')) return;

                if (!staticIPs.includes(ip)) {
                    const option = document.createElement('option');
                    option.value = ip;
                    option.textContent = ip;
                    listenAddressSelect.appendChild(option);
                }
            });
        }

        // Set the selected value from the config
        const currentListenAddress = proxyConf.listenAddress || '127.0.0.1';
        listenAddressSelect.value = currentListenAddress;

        // If the currently configured address wasn't in the list, add it as an option
        if (listenAddressSelect.value !== currentListenAddress) {
            const option = document.createElement('option');
            option.value = currentListenAddress;
            option.textContent = currentListenAddress + " (Saved, Not Detected)";
            listenAddressSelect.appendChild(option);
            listenAddressSelect.value = currentListenAddress;
        }

        // Populate Auto-enable Leader checkboxes
        if (proxyConf.heaterAutoEnableLeader) {
            const h0Auto = document.getElementById('heater-0-auto-enable-leader');
            const h1Auto = document.getElementById('heater-1-auto-enable-leader');
            if (h0Auto) h0Auto.checked = proxyConf.heaterAutoEnableLeader['pwm1'];
            if (h1Auto) h1Auto.checked = proxyConf.heaterAutoEnableLeader['pwm2'];
        }

        // Standardize switch map access (lowercase keys if needed, but usually they match)
        // Populate Custom PWM Names if present
        if (proxyConf.switchNames) {
            const pwm1Input = document.getElementById('heater-0-custom-name');
            if (pwm1Input) {
                const name = proxyConf.switchNames['pwm1'] || "";
                pwm1Input.value = name;
                updateHeaterHeader(0, name);
            }
            const pwm2Input = document.getElementById('heater-1-custom-name');
            if (pwm2Input) {
                const name = proxyConf.switchNames['pwm2'] || "";
                pwm2Input.value = name;
                updateHeaterHeader(1, name);
            }
        }

        if (proxyConf.switchNames) {
            // Update internal state
            window.currentSwitchNames = proxyConf.switchNames;

            // Note: Table population happens in fetchProxyConfig or fetchConfig when both data sources are ready.
            // But we can trigger it here if originalConfig is available
            if (originalConfig && originalConfig.ps) {
                populateSwitchConfigTable(window.currentSwitchNames, originalConfig.ps, originalConfig.av);
            }

            // Update Master Power label if configured
            if (proxyConf.switchNames['master_power']) {
                const masterLabel = document.getElementById('master-power-label');
                if (masterLabel) masterLabel.textContent = proxyConf.switchNames['master_power'];
                const masterInput = document.getElementById('proxy-master-power-name');
                if (masterInput) masterInput.value = proxyConf.switchNames['master_power'];
            }
        }

        // Toggle Master Power Visibility based on config
        const masterContainer = document.getElementById('master-switch-container');
        if (masterContainer) {
            // Default to visible if undefined
            const visible = (proxyConf.enableMasterPower !== false);
            masterContainer.style.display = visible ? 'flex' : 'none';
        }
    }

    async function fetchProxyConfig() {
        let statusOk = false;
        let proxyConfForStatus = null;
        try {
            // Fetch from the new, combined settings endpoint
            const response = await fetch('/api/v1/settings');
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            const settings = await response.json();
            const proxyConf = settings.proxy_config;
            const availableIPs = settings.available_ips;

            // Update Active Switches dynamically from backend!
            // Update Active Switches dynamically from backend!
            if (settings.active_switches) {
                switchIDMap = settings.active_switches;

                // Toggle Telemetry Tile Visibility
                const internalNames = Object.values(switchIDMap);
                const pwm1Status = document.getElementById('status-pwm1');
                const pwm2Status = document.getElementById('status-pwm2');

                if (pwm1Status) {
                    const tile = pwm1Status.closest('.status-item');
                    if (tile) tile.style.display = internalNames.includes('pwm1') ? 'flex' : 'none';
                }
                if (pwm2Status) {
                    const tile = pwm2Status.closest('.status-item');
                    if (tile) tile.style.display = internalNames.includes('pwm2') ? 'flex' : 'none';
                }
            }

            originalProxyConfig = JSON.parse(JSON.stringify(proxyConf));
            proxyConfForStatus = proxyConf;

            if (proxyConf) {
                applyProxyConfig(proxyConf, availableIPs);
                populatePowerControls(proxyConf.switchNames); // Restore power controls
                statusOk = true;
            }

        } catch (error) {
            console.error('Error fetching new proxy settings, trying fallback:', error);
            // Fallback to old endpoint if new one fails for graceful degradation
            try {
                const fallbackResponse = await fetch('/api/v1/proxy/config');
                if (!fallbackResponse.ok) throw new Error(`Fallback HTTP error! status: ${fallbackResponse.status}`);
                const proxyConf = await fallbackResponse.json();
                originalProxyConfig = JSON.parse(JSON.stringify(proxyConf));
                proxyConfForStatus = proxyConf;

                if (proxyConf) {
                    applyProxyConfig(proxyConf); // Call without IPs
                    statusOk = true;
                }
            } catch (fallbackError) {
                console.error('Error fetching proxy config (fallback):', fallbackError);
                const defaultNames = {};
                Object.values(switchIDMap).forEach(key => defaultNames[key] = key);
                // Fallback default names
                window.currentSwitchNames = defaultNames;
                if (originalConfig && originalConfig.ps) {
                    populateSwitchConfigTable(defaultNames, originalConfig.ps, originalConfig.av);
                }
            } finally {
                updateConnectionStatus(proxyConfForStatus, statusOk);

                // Update Switch Config Table if we have both configs
                if (proxyConfForStatus && proxyConfForStatus.switchNames) {
                    window.currentSwitchNames = proxyConfForStatus.switchNames;
                }
                if (originalConfig && originalConfig.ps) {
                    populateSwitchConfigTable(window.currentSwitchNames, originalConfig.ps, originalConfig.av);
                }
            }
        }
    }

    function populateSwitchConfigTable(switchNames, startupStates, adjVoltage) {
        const tbody = document.getElementById('switch-config-tbody');
        if (!tbody) return;
        tbody.innerHTML = '';

        const switchKeys = ["dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv"];
        const switchLabels = {
            "dc1": "DC 1", "dc2": "DC 2", "dc3": "DC 3", "dc4": "DC 4", "dc5": "DC 5",
            "usbc12": "USB-C 1/2", "usb345": "USB 3/4/5", "adj_conv": "Adj. Port"
        };
        const shortSwitchIDMap = {
            "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
            "usbc12": "u12", "usb345": "u34", "adj_conv": "adj"
        };


        switchKeys.forEach(key => {
            const row = document.createElement('tr');

            // 1. Name
            const nameCell = document.createElement('td');
            nameCell.textContent = switchLabels[key] || key;
            row.appendChild(nameCell);

            // 2. State (Startup)
            const stateCell = document.createElement('td');
            const select = document.createElement('select');
            select.dataset.key = key; // Firmware key (dc1 etc)
            select.className = "startup-state-select";

            // Options: 2=Disabled, 0=Off (Enabled), 1=On (Enabled)
            const optDisabled = document.createElement('option');
            optDisabled.value = "2";
            optDisabled.textContent = "Disabled";

            const optOff = document.createElement('option');
            optOff.value = "0";
            optOff.textContent = "Off (Enabled)";

            const optOn = document.createElement('option');
            optOn.value = "1";
            optOn.textContent = "On (Enabled)";

            // Set current value
            // startupStates[key] is now integer 0, 1, or 2 from firmware
            let currentVal = "0";
            const shortKey = shortSwitchIDMap[key];
            let rawState = undefined;

            if (startupStates) {
                if (shortKey && typeof startupStates[shortKey] !== 'undefined') rawState = startupStates[shortKey];
                else if (typeof startupStates[key] !== 'undefined') rawState = startupStates[key];
            }
            if (typeof rawState !== 'undefined') {
                currentVal = rawState.toString();
            }

            // Fallback for boolean-like behavior if firmware is mixed transition (shouldn't happen with uint8 conversion)
            if (currentVal === "true") currentVal = "1";
            if (currentVal === "false") currentVal = "0";

            select.appendChild(optOn);
            select.appendChild(optOff);
            select.appendChild(optDisabled);
            select.value = currentVal;

            if (key === 'master_power') {
                select.innerHTML = '';
                const optNA = document.createElement('option');
                optNA.value = "0";
                optNA.textContent = "N/A";
                select.appendChild(optNA);
                select.disabled = true;
                select.value = "0";
            }

            stateCell.appendChild(select);
            row.appendChild(stateCell);

            // 3. Custom Name
            const customNameCell = document.createElement('td');
            const nameInput = document.createElement('input');
            nameInput.type = 'text';
            nameInput.className = "custom-name-input";
            nameInput.dataset.key = key;
            nameInput.value = (switchNames && switchNames[key]) ? switchNames[key] : "";
            nameInput.placeholder = switchLabels[key];
            customNameCell.appendChild(nameInput);
            row.appendChild(customNameCell);

            // 4. Voltage (Adj Only)
            const voltCell = document.createElement('td');
            if (key === "adj_conv") {
                const voltInput = document.createElement('input');
                voltInput.type = 'number';
                voltInput.className = "adj-volt-input";
                voltInput.id = "adj-voltage-table-input"; // ID for easy saving
                voltInput.step = "0.1";
                voltInput.min = "3.0"; // Example limits
                voltInput.max = "12.0"; // Firmware might clamp differently
                voltInput.style.width = "100%"; // Fill cell
                if (typeof adjVoltage !== 'undefined') {
                    voltInput.value = adjVoltage;
                }
                voltCell.appendChild(voltInput);
            } else {
                // Placeholder input for consistency
                const placeholderInput = document.createElement('input');
                placeholderInput.type = 'text';
                placeholderInput.className = "volt-placeholder-input";
                placeholderInput.value = "-";
                placeholderInput.disabled = true;
                placeholderInput.style.width = "100%"; // Fill cell
                placeholderInput.style.textAlign = "center";
                voltCell.appendChild(placeholderInput);
            }
            row.appendChild(voltCell);

            tbody.appendChild(row);
        });
    }

    function updateHeaterHeader(index, customName) {
        const heaterDiv = document.getElementById(`heater-${index}`);
        if (!heaterDiv) return;
        const header = heaterDiv.querySelector('h4');
        if (!header) return;

        // Base name: Heater 1 (PWM1) or Heater 2 (PWM2)
        const base = index === 0 ? "Heater 1 (PWM1)" : "Heater 2 (PWM2)";
        if (customName && customName.trim() !== "") {
            header.textContent = `${base} - ${customName}`;
        } else {
            header.textContent = base;
        }
    }

    // --- Data Saving ---
    async function saveSensorSettings() {
        const newConfig = {
            so: { // sensor_offsets
                st: parseFloat(document.getElementById('offset-sht40-temp').value.replace(',', '.')),
                sh: parseFloat(document.getElementById('offset-sht40-humidity').value.replace(',', '.')),
                dt: parseFloat(document.getElementById('offset-ds18b20-temp').value.replace(',', '.')),
                iv: parseFloat(document.getElementById('offset-ina219-voltage').value.replace(',', '.')),
                ic: parseFloat(document.getElementById('offset-ina219-current').value.replace(',', '.'))
            },
            ui: { // update_intervals_ms
                i: parseInt(document.getElementById('interval-ina219').value, 10),
                s: parseInt(document.getElementById('interval-sht40').value, 10),
                d: parseInt(document.getElementById('interval-ds18b20').value, 10)
            },
            ac: { // averaging_counts
                st: parseInt(document.getElementById('avg-sht40-temp').value, 10),
                sh: parseInt(document.getElementById('avg-sht40-humidity').value, 10),
                dt: parseInt(document.getElementById('avg-ds18b20-temp').value, 10),
                iv: parseInt(document.getElementById('avg-ina219-voltage').value, 10),
                ic: parseInt(document.getElementById('avg-ina219-current').value, 10)
            }
        };

        try {
            if (connectionIndicator.className !== 'connected') {
                showAlert('Error', 'Cannot save device configuration: Device is not connected.');
                return;
            }
            showResponse("Saving sensor settings...");
            const response = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig)
            });
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            showResponse("Sensor settings saved successfully!");
            showAlert('Success', 'Sensor settings saved successfully!', 5000);
            resetUnsavedIndicators();
            fetchConfig();
        } catch (error) {
            console.error('Error saving sensor settings:', error);
            showResponse(`Error saving sensor settings: ${error.message}`, true);
            showAlert('Error', 'Error saving sensor settings.');
        }
    }

    async function saveDewHeaterSettings() {
        const dewHeatersConfig = [0, 1].map(i => {
            const mode = parseInt(document.getElementById(`heater-${i}-mode`).value, 10);
            let pidSettings = {
                to: parseFloat(document.getElementById(`heater-${i}-target-offset`).value.replace(',', '.')),
                kp: parseFloat(document.getElementById(`heater-${i}-pid-kp`).value.replace(',', '.')),
                ki: parseFloat(document.getElementById(`heater-${i}-pid-ki`).value.replace(',', '.')),
                kd: parseFloat(document.getElementById(`heater-${i}-pid-kd`).value.replace(',', '.'))
            };

            if (mode === 4) {
                pidSettings = {
                    to: parseFloat(document.getElementById(`heater-${i}-target-offset-4`).value.replace(',', '.')),
                    kp: parseFloat(document.getElementById(`heater-${i}-pid-kp-4`).value.replace(',', '.')),
                    ki: parseFloat(document.getElementById(`heater-${i}-pid-ki-4`).value.replace(',', '.')),
                    kd: parseFloat(document.getElementById(`heater-${i}-pid-kd-4`).value.replace(',', '.'))
                };
            }

            return {
                n: originalConfig.dh[i].n, // name
                en: document.getElementById(`heater-${i}-startup-enabled`).checked, // enabled_on_startup
                m: mode, // mode
                mp: parseInt(document.getElementById(`heater-${i}-manual-power`).value, 10) || 0, // manual_power
                ...pidSettings,
                sd: parseFloat(document.getElementById(`heater-${i}-start-delta`).value.replace(',', '.')), // start_delta
                ed: parseFloat(document.getElementById(`heater-${i}-end-delta`).value.replace(',', '.')), // end_delta
                xp: parseInt(document.getElementById(`heater-${i}-max-power`).value, 10) || 0, // max_power
                psf: parseFloat(document.getElementById(`heater-${i}-pid-sync-factor`).value.replace(',', '.')), // pid_sync_factor
                mt: parseFloat(document.getElementById(`heater-${i}-min-temp`).value.replace(',', '.')) // min_temp
            };
        });

        const newConfig = {
            dh: dewHeatersConfig // dew_heaters
        };

        try {
            if (connectionIndicator.className !== 'connected') {
                showAlert('Error', 'Cannot save device configuration: Device is not connected.');
                return;
            }
            showResponse("Saving dew heater settings...");
            const response = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig)
            });
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            showResponse("Dew heater settings saved successfully!");
            showAlert('Success', 'Dew heater settings saved successfully!', 5000);

            // After saving heater settings to the device, also save the proxy config
            // to ensure the "auto-enable leader" setting is persisted.
            await saveProxyConfig(true); // Pass true to suppress the alert
            resetUnsavedIndicators();
            fetchConfig();
        } catch (error) {
            console.error('Error saving dew heater settings:', error);
            showResponse(`Error saving dew heater settings: ${error.message}`, true);
            showAlert('Error', 'Error saving dew heater settings.');
        }
    }

    async function saveAdjustableVoltagePreset() {
        const newConfig = {
            av: parseFloat(document.getElementById('adj-voltage').value.replace(',', '.')) // adj_conv_preset_v
        };

        try {
            if (connectionIndicator.className !== 'connected') {
                showAlert('Error', 'Cannot save device configuration: Device is not connected.');
                return;
            }
            showResponse("Saving adjustable voltage preset...");
            const response = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig)
            });
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            showResponse("Adjustable voltage preset saved successfully!");
            showAlert('Success', 'Adjustable voltage preset saved successfully!', 5000);
            resetUnsavedIndicators();
            fetchConfig();
        } catch (error) {
            console.error('Error saving adjustable voltage preset:', error);
            showResponse(`Error saving adjustable voltage preset: ${error.message}`, true);
            showAlert('Error', 'Error saving adjustable voltage preset.');
        }
    }

    async function savePowerStartupStates() {
        const powerStartupStates = Array.from(document.querySelectorAll('#power-startup-states input[type="checkbox"]')).reduce((acc, cb) => {
            acc[cb.dataset.key] = cb.checked;
            return acc;
        }, {});

        const newConfig = {
            ps: powerStartupStates // power_startup_states
        };

        try {
            if (connectionIndicator.className !== 'connected') {
                alert('Cannot save device configuration: Device is not connected.');
                return;
            }
            showResponse("Saving power startup states...");
            const response = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig)
            });
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            showResponse("Power startup states saved successfully!");
            alert('Power startup states saved successfully!');
            resetUnsavedIndicators();
            fetchConfig();
        } catch (error) {
            console.error('Error saving power startup states:', error);
            showResponse(`Error saving power startup states: ${error.message}`, true);
            alert('Error saving power startup states.');
        }
    }

    async function saveProxyConfig(showAlert = true) {
        const newProxyConfig = {
            listenAddress: document.getElementById('proxy-listen-address').value,
            serialPortName: document.getElementById('proxy-serial-port').value.trim(),
            autoDetectPort: document.getElementById('proxy-auto-detect-port').checked,
            enableAlpacaVoltageControl: document.getElementById('proxy-enable-alpaca-voltage').checked,
            networkPort: parseInt(document.getElementById('proxy-network-port').value, 10) || 8080,
            logLevel: document.getElementById('proxy-log-level').value,
            historyRetentionNights: parseInt(document.getElementById('proxy-history-retention').value, 10) || 30,
            switchNames: {},
            heaterAutoEnableLeader: {
                'pwm1': document.getElementById('heater-0-auto-enable-leader').checked,
                'pwm2': document.getElementById('heater-1-auto-enable-leader').checked,
            }
        };
        document.querySelectorAll('#proxy-switch-names-grid input').forEach(input => {
            newProxyConfig.switchNames[input.dataset.key] = input.value.trim();
        });

        try {
            showResponse("Saving proxy settings...");
            // Post to the new endpoint. The body is just the config object.
            const response = await fetch('/api/v1/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newProxyConfig)
            });
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            showResponse("Proxy settings saved.");
            if (showAlert) {
                alert("Proxy settings saved. Some changes may require an application restart.");
            }
            resetUnsavedIndicators();
            fetchProxyConfig(); // Re-fetch to update originalProxyConfig and UI
        } catch (error) {
            console.error('Error saving proxy config:', error);
            showResponse(`Error saving proxy config: ${error.message}`, true);
            alert('Error saving proxy connection settings.');
        }
    }

    function setupSaveButtons() {
        // Dew Heater Save Button - now at the bottom of the tab
        const saveHeatersBtn = document.querySelector('#tab-heaters .save-config-button');
        if (saveHeatersBtn) saveHeatersBtn.addEventListener('click', saveDewHeaterSettings);

        // Sensor Settings Save Button
        const saveSensorsBtn = document.querySelector('#tab-sensors .save-config-button');
        if (saveSensorsBtn) saveSensorsBtn.addEventListener('click', saveSensorSettings);

        // Auto-Drying Save
        const saveAutoDryBtn = document.querySelector('#tab-sensors .settings-block:last-child .save-config-button');
        if (saveAutoDryBtn) saveAutoDryBtn.addEventListener('click', saveAutoDryingSettings);

    }

    async function saveAutoDryingSettings() {
        const newConfig = {
            ad: { // auto_dry
                en: document.getElementById('auto-dry-enabled').checked,
                ht: parseInt(document.getElementById('auto-dry-humidity-threshold').value, 10),
                td: parseInt(document.getElementById('auto-dry-trigger-duration').value, 10)
            }
        };

        try {
            if (connectionIndicator.className !== 'connected') {
                alert('Cannot save device configuration: Device is not connected.');
                return;
            }
            showResponse("Saving auto-drying settings...");
            const response = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig)
            });
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

            showResponse("Auto-drying settings saved successfully!");
            alert('Auto-drying settings saved successfully!');
            resetUnsavedIndicators();
            fetchConfig();
        } catch (error) {
            console.error('Error saving auto-drying settings:', error);
            showResponse(`Error saving auto-drying settings: ${error.message}`, true);
            alert('Error saving auto-drying settings.');
        }
    }

    async function triggerManualDrySensor() {
        if (!confirm('This will activate the sensor heater for a short period, temporarily affecting ambient readings. Proceed?')) return;
        try {
            showResponse("Sending sensor drying command...");
            const response = await fetch('/api/v1/command', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ command: 'dry_sensor' })
            });
            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Server responded with ${response.status}: ${errorText}`);
            }
            const result = await response.json();
            showResponse(`Sensor drying cycle initiated. Device response: ${result.status || 'OK'}`);
            alert('Sensor drying cycle initiated successfully.');
        } catch (error) {
            showResponse(`Error sending command: ${error.message}`, true);
            alert(`Error sending command: ${error.message}`);
        }
    }

    // --- Power Control ---
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

    async function setPowerState(switchId, state) {
        const formData = new URLSearchParams();
        formData.append('Id', switchId);
        formData.append('State', state);
        try {
            await fetch('/api/v1/switch/0/setswitchvalue', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: formData
            });
        } catch (error) {
            console.error(`Error setting power state for switch ${switchId}:`, error);
            fetchPowerStatus();
        }
    }

    async function fetchPowerStatus() {
        try {
            const response = await fetch('/api/v1/power/status');
            if (!response.ok) return;
            const status = await response.json();
            let allOn = true;
            for (const [key, value] of Object.entries(status)) {
                const switchInput = document.querySelector(`.switch-toggle input[data-key='${key}']`);
                if (switchInput) {
                    // Check if value is "truthy" (1 or boolean true or > 0 for voltage)
                    const isPyhsicallyOn = (typeof value === 'boolean' && value) || (typeof value === 'number' && value > 0);
                    switchInput.checked = isPyhsicallyOn;
                    if (!isPyhsicallyOn) allOn = false;
                }
            }
            if (masterPowerSwitch) masterPowerSwitch.checked = allOn;
        } catch (error) {
            // console.error('Error fetching power status:', error);
        }
    }

    function populatePowerControls(switchNames) {
        const grid = document.getElementById('power-grid');
        if (!grid) return;
        const shortSwitchIDMap = {
            "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
            "usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
        };
        grid.innerHTML = '';
        for (const [id, key] of Object.entries(switchIDMap)) {
            // Skip Master Power in the grid (it has its own dedicated control)
            if (key === 'master_power') continue;

            const shortKey = shortSwitchIDMap[key] || key;

            // Filter disabled switches (State 2)
            // We check originalConfig.ps (Power Startup States) which holds the mode (0=Off, 1=On, 2=Disabled)
            if (originalConfig && originalConfig.ps && originalConfig.ps[shortKey] === 2) {
                continue;
            }

            // Filter disabled heaters (Mode 5)
            // Heaters are in originalConfig.h array. pwm1 -> index 0, pwm2 -> index 1
            if (originalConfig && originalConfig.h) {
                if (key === 'pwm1' && originalConfig.h[0] && originalConfig.h[0].m === 5) continue;
                if (key === 'pwm2' && originalConfig.h[1] && originalConfig.h[1].m === 5) continue;
            }

            const displayName = (switchNames && switchNames[key]) ? switchNames[key] : (longToShortKeyMap[key] || key);
            const controlDiv = document.createElement('div');
            controlDiv.className = 'switch-control glass-panel'; // CSS class handles the rest

            const nameSpan = document.createElement('span');
            nameSpan.className = 'name';
            nameSpan.textContent = displayName;

            const switchLabel = document.createElement('label');
            switchLabel.className = 'switch-toggle';
            const input = document.createElement('input');
            input.type = 'checkbox';
            input.dataset.key = shortKey;
            input.dataset.id = id;
            input.addEventListener('change', (e) => setPowerState(e.target.dataset.id, e.target.checked));
            const sliderSpan = document.createElement('span');
            sliderSpan.className = 'slider';
            switchLabel.appendChild(input);
            switchLabel.appendChild(sliderSpan);

            controlDiv.appendChild(nameSpan);
            controlDiv.appendChild(switchLabel);
            grid.appendChild(controlDiv);
        }
    }

    // --- UI and Event Handlers ---
    function setupCollapsible() {
        document.querySelectorAll('.collapsible-header').forEach(header => {
            const parent = header.parentElement;
            const content = header.nextElementSibling;
            const icon = header.querySelector('.toggle-icon');
            if (!content || !icon || !parent || !parent.id) return;

            const storageKey = `collapsed-${parent.id}`;
            const isCollapsed = localStorage.getItem(storageKey) === 'true';

            // Apply initial state
            if (isCollapsed) {
                content.classList.add('collapsed');
                icon.classList.add('collapsed');
            }

            header.addEventListener('click', () => {
                const currentlyCollapsed = content.classList.toggle('collapsed');
                icon.classList.toggle('collapsed', currentlyCollapsed);
                localStorage.setItem(storageKey, currentlyCollapsed);
            });
        });
    }

    // --- Tab Switching Logic (New) ---
    function setupTabs() {
        const tabs = document.querySelectorAll('.tab-btn');
        const contents = document.querySelectorAll('.tab-content');

        tabs.forEach(tab => {
            tab.addEventListener('click', () => {
                // Deactivate all
                tabs.forEach(t => t.classList.remove('active'));
                contents.forEach(c => c.classList.remove('active'));

                // Activate clicked
                tab.classList.add('active');
                const targetId = tab.dataset.tab;
                const targetContent = document.getElementById(targetId);
                if (targetContent) {
                    targetContent.classList.add('active');
                }
            });
        });
    }

    async function fetchLiveStatus() {
        let statusOk = false;
        try {
            const response = await fetch('/api/v1/status');
            if (!response.ok) throw new Error('No response');
            const data = await response.json();
            if (Object.keys(data).length === 0) throw new Error('Empty response');

            const format = (elId, value, points) => {
                const el = document.getElementById(elId);
                if (el) {
                    let displayVal = 'N/A';
                    if (value !== null && typeof value !== 'undefined') {
                        displayVal = (typeof value === 'number') ? value.toFixed(points) : value;
                    }

                    // Preserve the unit (<small> tag)
                    const unit = el.querySelector('small')?.outerHTML || '';
                    el.innerHTML = `${displayVal} ${unit}`;
                }
            };

            format('status-v', data.v, 2);
            format('status-i', (data.i === null || typeof data.i === 'undefined') ? null : data.i / 1000, 2);
            format('status-p', data.p, 2);
            format('status-t_amb', data.t_amb, 1);
            format('status-h_amb', data.h_amb, 1);
            format('status-d', data.d, 1);
            format('status-t_lens', data.t_lens, 1);
            format('status-pwm1', data.pwm1, 0);
            format('status-pwm2', data.pwm2, 0);

            statusOk = true;
        } catch (error) {
            // console.error(error);
        }
        updateConnectionStatus(originalProxyConfig, statusOk);
        fetchPowerStatus(); // Ensure switch states are updated periodically
    }

    function setupLogViewer() {
        const logContainer = document.getElementById('log-container');
        const maxLogLines = 500;
        let isAutoScroll = true;

        // Detect user scroll to disable auto-scroll
        logContainer.addEventListener('scroll', () => {
            const threshold = 50;
            const position = logContainer.scrollTop + logContainer.clientHeight;
            const height = logContainer.scrollHeight;
            isAutoScroll = position > height - threshold;
        });

        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        // Use a relative path for flexibility, or configurable port
        const wsUrl = `${wsProtocol}//${window.location.hostname}:${window.location.port}/ws/logs`;

        let socket;

        function connect() {
            socket = new WebSocket(wsUrl);

            socket.onopen = () => {
                console.log('Log WebSocket Connected');
            };

            socket.onmessage = (event) => {
                const message = event.data;
                const line = document.createElement('div');
                line.textContent = message; // Safe text

                // Simple coloring based on content
                if (message.includes('ERROR')) line.className = 'log-line-error';
                else if (message.includes('WARN')) line.className = 'log-line-warn';
                else if (message.includes('DEBUG')) line.className = 'log-line-debug';

                logContainer.appendChild(line);

                // Prune old lines
                while (logContainer.children.length > maxLogLines) {
                    logContainer.removeChild(logContainer.firstChild);
                }

                if (isAutoScroll) {
                    logContainer.scrollTop = logContainer.scrollHeight;
                }
            };

            socket.onclose = () => {
                console.log('Log WebSocket Disconnected. Reconnecting in 2s...');
                setTimeout(connect, 2000);
            };

            socket.onerror = (error) => {
                console.error('WebSocket Error:', error);
                // socket.close();
            };
        }

        if (logContainer) {
            connect();
        }
    }

    async function saveSwitchConfig() {
        const tbody = document.getElementById('switch-config-tbody');
        if (!tbody) return;

        // 1. Gather Data
        // Initialize with existing names to preserve those not in the table (e.g. Heaters)
        const gatheredProxyNames = (originalProxyConfig && originalProxyConfig.switchNames)
            ? JSON.parse(JSON.stringify(originalProxyConfig.switchNames))
            : {};

        const gatheredStartupStates = {};
        let adjVoltage = null;

        const shortSwitchIDMap = {
            "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
            "usbc12": "u12", "usb345": "u34", "adj_conv": "adj"
        };

        const rows = tbody.querySelectorAll('tr');
        rows.forEach(row => {
            const select = row.querySelector('.startup-state-select');
            const nameInput = row.querySelector('.custom-name-input');
            const voltInput = row.querySelector('.adj-volt-input');

            if (select && nameInput) {
                const key = select.dataset.key;
                const state = parseInt(select.value, 10);
                const customName = nameInput.value.trim();

                // Startup State (0, 1, 2)
                // Filter out master_power as it is Proxy-only
                if (key !== 'master_power') {
                    const shortKey = shortSwitchIDMap[key] || key;
                    gatheredStartupStates[shortKey] = state;
                }

                // Custom Name (Proxy)
                gatheredProxyNames[key] = customName;
            }

            if (voltInput) {
                adjVoltage = parseFloat(voltInput.value);
            }
        });

        // 2. Save Firmware Settings (Startup States + Voltage)
        try {
            if (connectionIndicator.className !== 'connected') {
                showAlert('Error', 'Cannot save configuration: Device is not connected.');
                return;
            }

            const firmwareConfig = {
                ps: gatheredStartupStates, // power_startup_states
                av: adjVoltage // adj_conv_preset_v
            };

            showResponse("Saving firmware switch settings...");
            const fwResponse = await fetch('/api/v1/config/set', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(firmwareConfig)
            });
            if (!fwResponse.ok) throw new Error(`Firmware Config Save failed: ${fwResponse.status}`);

            // 3. Save Proxy Settings (Names)
            // We need to merge with existing names to avoid losing others if any
            const newProxyConfig = { ...originalProxyConfig };
            newProxyConfig.switchNames = gatheredProxyNames;

            showResponse("Saving custom names...");
            const proxyResponse = await fetch('/api/v1/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newProxyConfig)
            });
            if (!proxyResponse.ok) throw new Error(`Proxy Config Save failed: ${proxyResponse.status}`);

            showResponse("All switch settings saved successfully!");
            showAlert('Success', "Switch configuration saved successfully!", 5000);
            setTimeout(() => location.reload(), 5100); // Reload to refresh the switch list/telemetry based on new "Disabled" states
        } catch (error) {
            console.error(error);
            showResponse(`Error saving settings: ${error.message}`, true);
            showAlert('Error', `Error saving settings: ${error.message}`);
        }
    }

    async function saveProxyConfig(suppressFeedback = false) {
        if (!originalProxyConfig) return;

        // constant deep copy to prevent mutation of the original until saved
        const newConfig = JSON.parse(JSON.stringify(originalProxyConfig));

        // serial
        const serialPortInput = document.getElementById('proxy-serial-port');
        if (serialPortInput) newConfig.serialPortName = serialPortInput.value.trim();

        const autoDetectInput = document.getElementById('proxy-auto-detect-port');
        if (autoDetectInput) newConfig.autoDetectPort = autoDetectInput.checked;

        // alpaca
        const alpacaVoltageInput = document.getElementById('proxy-enable-alpaca-voltage');
        if (alpacaVoltageInput) newConfig.enableAlpacaVoltageControl = alpacaVoltageInput.checked;

        const masterPowerStateInput = document.getElementById('proxy-master-power-state');
        if (masterPowerStateInput) newConfig.enableMasterPower = (masterPowerStateInput.value === 'enabled');

        const networkPortInput = document.getElementById('proxy-network-port');
        if (networkPortInput) newConfig.networkPort = parseInt(networkPortInput.value, 10);

        const logLevelInput = document.getElementById('proxy-log-level');
        if (logLevelInput) newConfig.logLevel = logLevelInput.value;

        const listenAddrInput = document.getElementById('proxy-listen-address');
        if (listenAddrInput) newConfig.listenAddress = listenAddrInput.value;

        const historyRetentionInput = document.getElementById('proxy-history-retention');
        if (historyRetentionInput) newConfig.historyRetentionNights = parseInt(historyRetentionInput.value, 10);

        // heater auto
        if (!newConfig.heaterAutoEnableLeader) newConfig.heaterAutoEnableLeader = {};
        const heater0Auto = document.getElementById('heater-0-auto-enable-leader');
        if (heater0Auto) newConfig.heaterAutoEnableLeader['pwm1'] = heater0Auto.checked;

        const heater1Auto = document.getElementById('heater-1-auto-enable-leader');
        if (heater1Auto) newConfig.heaterAutoEnableLeader['pwm2'] = heater1Auto.checked;

        // master power name
        const masterNameInput = document.getElementById('proxy-master-power-name');
        if (masterNameInput) {
            const masterName = masterNameInput.value.trim();
            if (!newConfig.switchNames) newConfig.switchNames = {};
            // Preserve existing switch names by strictly assigning only master_power
            if (masterName) {
                newConfig.switchNames['master_power'] = masterName;
            } else {
                newConfig.switchNames['master_power'] = "";
            }
        }

        // PWM Custom Names
        const pwm1NameInput = document.getElementById('heater-0-custom-name');
        if (pwm1NameInput) {
            if (!newConfig.switchNames) newConfig.switchNames = {};
            newConfig.switchNames['pwm1'] = pwm1NameInput.value.trim();
        }
        const pwm2NameInput = document.getElementById('heater-1-custom-name');
        if (pwm2NameInput) {
            if (!newConfig.switchNames) newConfig.switchNames = {};
            newConfig.switchNames['pwm2'] = pwm2NameInput.value.trim();
        }

        try {
            if (!suppressFeedback) showResponse("Saving Proxy configuration...");

            const res = await fetch('/api/v1/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig)
            });
            if (!res.ok) throw new Error("Save failed");

            // Update original config to match what we just saved (to keep state in sync without reload if suppressed)
            originalProxyConfig = newConfig;

            if (!suppressFeedback) {
                showResponse("Proxy settings saved. Restarting proxy might be required for some changes.");
                showAlert("Success", "Proxy configuration saved! Restarting proxy might be required for some changes.", 5000);
                setTimeout(() => location.reload(), 5100);
            } else {
                console.log("Proxy config saved silently.");
            }
        } catch (e) {
            console.error(e);
            if (!suppressFeedback) showAlert("Error", "Error saving proxy settings: " + e.message);
            else console.error("Error saving proxy settings:", e.message);
        }
    }

    // --- Initialization ---
    async function init() {
        setupTabs(); // New tab handler
        setupCollapsible();

        // Load configs
        await fetchProxyConfig();
        await fetchConfig();
        await fetchFirmwareVersion();
        await fetchProxyVersion();

        // Setup specific event listeners
        setupChangeDetection();
        setupSaveButtons();

        // Switch Configuration (Unified)
        const saveSwitchBtn = document.getElementById('save-switch-config-button');
        if (saveSwitchBtn) saveSwitchBtn.addEventListener('click', saveSwitchConfig);

        const saveProxyBtn = document.getElementById('save-proxy-config-button');
        if (saveProxyBtn) saveProxyBtn.addEventListener('click', () => saveProxyConfig(false));

        // Other buttons
        const rebootBtn = document.getElementById('reboot-button');
        if (rebootBtn) {
            rebootBtn.addEventListener('click', () => {
                showConfirm('Reboot Device', 'Are you sure you want to reboot the device?', async () => {
                    try {
                        await fetch('/api/v1/command', { method: 'POST', body: JSON.stringify({ command: 'reboot' }) });
                        showAlert('Success', 'Reboot command sent.', 5000);
                    } catch (e) { showAlert('Error', 'Error sending reboot command'); }
                });
            });
        }

        const factoryResetBtn = document.getElementById('factory-reset-button');
        if (factoryResetBtn) {
            factoryResetBtn.addEventListener('click', () => {
                showConfirm('Factory Reset', 'WARNING: This will reset all settings to factory defaults. Continue?', async () => {
                    try {
                        await fetch('/api/v1/command', { method: 'POST', body: JSON.stringify({ command: 'factory_reset' }) });
                        showAlert('Success', 'Factory reset command sent. Device will reboot.', 5000);
                    } catch (e) { showAlert('Error', 'Error sending factory reset command'); }
                });
            });
        }

        const backupBtn = document.getElementById('backup-button');
        if (backupBtn) {
            backupBtn.addEventListener('click', () => {
                window.location.href = '/api/v1/backup/create';
            });
        }

        const manualDryBtn = document.getElementById('manual-dry-sensor-button');
        if (manualDryBtn) manualDryBtn.addEventListener('click', triggerManualDrySensor);

        if (masterPowerSwitch) {
            masterPowerSwitch.addEventListener('change', (e) => {
                setAllPower(e.target.checked);
            });
        }

        // File Restore
        if (restoreButton) restoreButton.addEventListener('click', () => restoreFileInput.click());
        if (restoreFileInput) {
            restoreFileInput.addEventListener('change', async (e) => {
                const file = e.target.files[0];
                if (!file) return;
                const reader = new FileReader();
                reader.onload = async (e) => {
                    try {
                        const config = JSON.parse(e.target.result);
                        await fetch('/api/v1/backup/restore', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify(config)
                        });
                        alert('Configuration restored successfully! Reloading...');
                        location.reload();
                    } catch (err) {
                        alert('Error restoring configuration: ' + err.message);
                    }
                };
                reader.readAsText(file);
            });
        }

        // Heaters Interactivity
        ['heater-0-mode', 'heater-1-mode'].forEach(id => {
            const el = document.getElementById(id);
            if (el) {
                el.addEventListener('change', (e) => {
                    const index = id.includes('0') ? 0 : 1;
                    updateHeaterModeView(index);
                    updateHeaterOptions();
                });
            }
        });

        // Periodic Updates
        setInterval(fetchLiveStatus, 2000);
        fetchLiveStatus(); // Initial call
        setupLogViewer();
    }

    init();

    // --- Telemetry Chart Logic ---
    let telemetryChart = null;
    let currentMetric = 'v'; // Default to voltage
    let currentMetricLabel = 'Voltage';

    const modal = document.getElementById('telemetry-modal');
    const closeModalBtn = document.getElementById('close-telemetry-modal');
    const dateSelect = document.getElementById('telemetry-date-select');

    // Sensor Configuration for Telemetry
    const sensorMap = [
        { id: 'status-v', metric: 'v', label: 'Voltage (V)' },
        { id: 'status-i', metric: 'c', label: 'Current (A)' },
        { id: 'status-p', metric: 'p', label: 'Power (W)' },
        { id: 'status-t_amb', metric: 'temp', label: 'Ambient Temp (°C)' },
        { id: 'status-h_amb', metric: 'hum', label: 'Humidity (%)' },
        { id: 'status-d', metric: 'dew', label: 'Dew Point (°C)' },
        { id: 'status-t_lens', metric: 'lens', label: 'Lens Temp (°C)' },
        { id: 'status-pwm1', metric: 'pwm1', label: 'PWM 1 (%)' },
        { id: 'status-pwm2', metric: 'pwm2', label: 'PWM 2 (%)' },
    ];

    sensorMap.forEach(sensor => {
        const el = document.getElementById(sensor.id);
        if (el) {
            el.classList.add('clickable-sensor');
            el.addEventListener('click', () => openTelemetryModal(sensor.metric, sensor.label));
        }
    });

    // Modal Controls
    if (closeModalBtn) {
        closeModalBtn.addEventListener('click', closeTelemetryModal);
    }

    // Close on click outside
    if (modal) {
        modal.addEventListener('click', (e) => {
            if (e.target === modal) closeTelemetryModal();
        });
    }

    if (dateSelect) {
        dateSelect.addEventListener('change', (e) => {
            loadTelemetryData(e.target.value);
        });
    }

    async function openTelemetryModal(metric, title) {
        currentMetric = metric;
        currentMetricLabel = title;
        document.getElementById('telemetry-modal-title').textContent = `${title} History`;

        // Populate dates
        await loadDateOptions();

        // Show modal
        modal.classList.remove('hidden');

        // Load initial data (current log)
        dateSelect.value = ""; // Default to current
        loadTelemetryData("");
    }

    function closeTelemetryModal() {
        modal.classList.add('hidden');
        if (telemetryChart) {
            telemetryChart.destroy();
            telemetryChart = null;
        }
    }

    // --- Generic Modals ---
    window.closeModal = function (id) {
        document.getElementById(id).classList.add('hidden');
    };

    function showConfirm(title, message, callback) {
        const modal = document.getElementById('confirmation-modal');
        document.getElementById('confirm-modal-title').textContent = title;
        document.getElementById('confirm-modal-message').textContent = message;

        const okBtn = document.getElementById('confirm-modal-ok');
        // Remove old listeners to prevent stacking
        const newBtn = okBtn.cloneNode(true);
        okBtn.parentNode.replaceChild(newBtn, okBtn);

        newBtn.addEventListener('click', () => {
            closeModal('confirmation-modal');
            if (callback) callback();
        });

        modal.classList.remove('hidden');
    }

    function showAlert(title, message, autoCloseDuration = 0) {
        const modal = document.getElementById('alert-modal');
        document.getElementById('alert-modal-title').textContent = title;
        document.getElementById('alert-modal-message').textContent = message;

        const okBtn = modal.querySelector('.btn-primary');
        okBtn.style.display = 'inline-block'; // Always show button

        if (autoCloseDuration > 0) {
            setTimeout(() => {
                // Check if modal is still open before closing (simple check)
                if (!modal.classList.contains('hidden')) {
                    closeModal('alert-modal');
                }
            }, autoCloseDuration);
        }

        modal.classList.remove('hidden');
    }



    async function loadDateOptions() {
        dateSelect.innerHTML = '<option value="">Current & Previous Night</option>';
        try {
            const res = await fetch('/api/v1/telemetry/dates');
            if (res.ok) {
                const dates = await res.json();
                dates.forEach(date => {
                    const opt = document.createElement('option');
                    opt.value = date;
                    opt.textContent = date;
                    dateSelect.appendChild(opt);
                });
            }
        } catch (e) {
            console.error("Failed to load dates", e);
        }
    }

    async function loadTelemetryData(dateParam) {
        let url = '/api/v1/telemetry/history';
        if (dateParam) {
            url += `?date=${dateParam}`;
        }

        try {
            const res = await fetch(url);
            if (!res.ok) throw new Error("Failed to fetch history");
            const data = await res.json();
            renderChart(data);
        } catch (e) {
            console.error(e);
            alert("Could not load telemetry data");
        }
    }

    function renderChart(data) {
        const ctx = document.getElementById('telemetry-chart').getContext('2d');

        // Destroy existing
        if (telemetryChart) telemetryChart.destroy();

        // Prepare data
        // API returns timestamp in seconds (Unix). Chart.js needs millis or ISO string.
        // Assuming DataPoint.Timestamp is int64 Unix seconds.
        const chartData = data.map(dp => {
            let val = dp[currentMetric];
            if (currentMetric === 'c') {
                val = val / 1000.0;
            }
            return {
                x: dp.t * 1000,
                y: val
            };
        }).filter(pt => !isNaN(pt.y)); // Filter out nulls/NaNs

        telemetryChart = new Chart(ctx, {
            type: 'line',
            data: {
                datasets: [{
                    label: currentMetricLabel,
                    data: chartData,
                    borderColor: '#00d2ff',
                    backgroundColor: 'rgba(0, 210, 255, 0.1)',
                    borderWidth: 2,
                    pointRadius: 0, // Hide points for performance on large datasets
                    pointHitRadius: 10,
                    fill: true,
                    tension: 0.1
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            displayFormats: {
                                hour: 'HH:mm',
                                minute: 'HH:mm'
                            },
                            tooltipFormat: 'yyyy-MM-dd HH:mm:ss'
                        },
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: { color: '#ccc' }
                    },
                    y: {
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: { color: '#ccc' }
                    }
                },
                plugins: {
                    legend: { display: false },
                    zoom: {
                        pan: { enabled: true, mode: 'x' },
                        zoom: {
                            wheel: { enabled: true },
                            pinch: { enabled: true },
                            mode: 'x',
                        }
                    }
                },
                interaction: {
                    mode: 'index',
                    intersect: false
                }
            }
        });

        // Double click to reset zoom
        document.getElementById('telemetry-chart').ondblclick = () => telemetryChart.resetZoom();
    }
});