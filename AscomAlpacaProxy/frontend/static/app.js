        document.addEventListener('DOMContentLoaded', () => {
            // --- DOM Elements ---
            const comPortElement = document.getElementById('com-port');
            const deviceResponseElement = document.getElementById('device-response');
            const connectionIndicator = document.getElementById('connection-indicator');
            const masterPowerSwitch = document.getElementById('master-power-switch');
            const rebootButton = document.getElementById('reboot-button');
            const factoryResetButton = document.getElementById('factory-reset-button');
            const backupButton = document.getElementById('backup-button');
            const restoreButton = document.getElementById('restore-button');
            const restoreFileInput = document.getElementById('restore-file-input');
            const firmwareVersionElement = document.getElementById('firmware-version');

            // --- State ---
            let originalConfig = {};
            let originalProxyConfig = {};
            const switchIDMap = {
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
                    document.querySelectorAll('.save-config-button').forEach(btn => btn.disabled = true);
                    comPortElement.textContent = "Proxy Offline";
                    return;
                }

                if (proxyConf.serialPortName) {
                    if (statusOk) {
                        document.querySelectorAll('.save-config-button').forEach(btn => btn.disabled = false);
                        connectionIndicator.className = 'connected';
                        comPortElement.textContent = proxyConf.serialPortName;
                    } else {
                        document.querySelectorAll('.save-config-button').forEach(btn => btn.disabled = true);
                        connectionIndicator.className = 'disconnected';
                        comPortElement.textContent = `Connecting to ${proxyConf.serialPortName}...`;
                    }
                } else {
                    connectionIndicator.className = 'connecting';
                    comPortElement.textContent = "Auto-detecting...";
                }

            }

            // --- Unsaved Changes ---
            function setupChangeDetection() {
                const configForm = document.getElementById('config-form');
                configForm.addEventListener('input', (e) => {
                    const changedSection = e.target.closest('.config-group') || e.target.closest('.collapsible-content');

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
                const currentHeaterModeSelect = document.getElementById(`heater-${heaterIndex}-mode`);
                const selectedMode = currentHeaterModeSelect.value;

                // Hide/show settings blocks for the current heater
                document.querySelectorAll(`#heater-${heaterIndex} .mode-settings`).forEach(el => el.classList.remove('active'));
                const activeSettings = document.getElementById(`heater-${heaterIndex}-mode-${selectedMode}`);
                if (activeSettings) activeSettings.classList.add('active');

                // --- New logic to control the other heater's options ---
                const otherHeaterIndex = 1 - heaterIndex;
                const otherHeaterModeSelect = document.getElementById(`heater-${otherHeaterIndex}-mode`);
                const otherHeaterSyncOption = otherHeaterModeSelect.querySelector('option[value="3"]');

                // If the current heater is in PID mode, the other can be a follower. Otherwise, it cannot.
                const isLeaderInPidMode = (currentHeaterModeSelect.value === '1');
                otherHeaterSyncOption.disabled = !isLeaderInPidMode;

                // If the other heater was a follower but its leader is no longer in PID mode, reset it to manual.
                if (!isLeaderInPidMode && otherHeaterModeSelect.value === '3') {
                    otherHeaterModeSelect.value = '0'; // Reset to Manual
                    // Trigger an update for the other heater as well to reflect the change
                    updateHeaterModeView(otherHeaterIndex); 
                }
            }

            // --- Data Fetching and Population ---
            async function fetchConfig() {
                try {
                    const response = await fetch('/api/v1/config'); 
                    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                    const config = await response.json();
                    originalConfig = JSON.parse(JSON.stringify(config)); // Deep copy
                    
                    document.getElementById('adj-voltage').value = config.av !== undefined ? config.av : ''; // Behandelt 'av', wenn es undefined ist

                    if (config.so) { // sensor_offsets -> so
                        document.getElementById('offset-sht40-temp').value = config.so.st;
                        document.getElementById('offset-sht40-humidity').value = config.so.sh;
                        document.getElementById('offset-ds18b20-temp').value = config.so.dt;
                        document.getElementById('offset-ina219-voltage').value = config.so.iv;
                        document.getElementById('offset-ina219-current').value = config.so.ic;
                    }

                    if (config.ps) { // power_startup_states -> ps
                        for (const [key, value] of Object.entries(config.ps)) {
                            const checkbox = document.querySelector(`#power-startup-states input[data-key="${key.toLowerCase()}"]`);
                            if (checkbox) checkbox.checked = value;
                        }
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
                            updateHeaterModeView(i);
                        });
                    }
                    
                    deviceResponseElement.textContent = "Configuration loaded successfully.";
                    return true;
                } catch (error) {
                    console.error('Error fetching config:', error);
                    deviceResponseElement.textContent = `Error fetching config: ${error.message}`;
                    return false;
                }
            }

            async function fetchProxyConfig() {
                let statusOk = false;
                try {
                    const response = await fetch('/api/v1/proxy/config');
                    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                    const proxyConf = await response.json();
                    originalProxyConfig = JSON.parse(JSON.stringify(proxyConf));
                    
                    if (proxyConf) {
                        applyProxyConfig(proxyConf);
                        statusOk = true;
                    }
                    updateConnectionStatus(proxyConf, statusOk);
                } catch (error) {
                    console.error('Error fetching proxy config:', error);
                    updateConnectionStatus(null, false);
                    const defaultNames = {};
                    Object.values(switchIDMap).forEach(key => defaultNames[key] = key);
                    populateSwitchNameInputs(defaultNames);
                    populatePowerControls(defaultNames);
                    populateStartupStateLabels(defaultNames);
                }

                // This function applies the proxy config to the UI elements.
                // It's separated so it can be called after the device config is also loaded.
                function applyProxyConfig(proxyConf) {
                    document.getElementById('proxy-serial-port').value = proxyConf.serialPortName || '';
                    document.getElementById('proxy-auto-detect-port').checked = proxyConf.autoDetectPort;

                    // Disable serial port input if auto-detect is on
                    document.getElementById('proxy-serial-port').disabled = proxyConf.autoDetectPort;

                    document.getElementById('proxy-network-port').value = proxyConf.networkPort || 8080;
                    document.getElementById('proxy-log-level').value = proxyConf.logLevel || 'INFO';

                    if (proxyConf.heaterAutoEnableLeader) {
                        document.getElementById('heater-0-auto-enable-leader').checked = proxyConf.heaterAutoEnableLeader['pwm1'];
                        document.getElementById('heater-1-auto-enable-leader').checked = proxyConf.heaterAutoEnableLeader['pwm2'];
                    }

                    if (proxyConf.switchNames) {
                        populateSwitchNameInputs(proxyConf.switchNames);
                        populatePowerControls(proxyConf.switchNames);
                        populateStartupStateLabels(proxyConf.switchNames);
                    }
                }
            }

            function populateSwitchNameInputs(switchNames) {
                const grid = document.getElementById('proxy-switch-names-grid');
                grid.innerHTML = '';
                for (const [key, name] of Object.entries(switchNames)) {
                    const label = document.createElement('label');
                    label.textContent = `${key}: `;
                    const input = document.createElement('input');
                    input.type = 'text';
                    input.dataset.key = key;
                    input.value = name;
                    grid.appendChild(label).appendChild(input);
                }
            }

            function populateStartupStateLabels(switchNames) {
                const grid = document.getElementById('power-startup-states');
                const shortSwitchIDMap = {
                    "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
                    "usbc12": "u12", "usb345": "u34", "adj_conv": "adj"
                };
                grid.innerHTML = '';
                const startupStateKeys = ["dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv"];
                startupStateKeys.forEach(key => {
                    const shortKey = shortSwitchIDMap[key] || key;
                    const displayName = (switchNames && switchNames[key]) ? switchNames[key] : (longToShortKeyMap[key] || key);
                    const label = document.createElement('label');
                    const input = document.createElement('input');
                    input.type = 'checkbox';
                    input.dataset.key = shortKey;
                    label.appendChild(input);
                    label.appendChild(document.createTextNode(` ${displayName}`));
                    grid.appendChild(label);
                });
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
                        alert('Cannot save device configuration: Device is not connected.');
                        return;
                    }
                    deviceResponseElement.textContent = "Saving sensor settings...";
                    const response = await fetch('/api/v1/config/set', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(newConfig)
                    });
                    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                    
                    deviceResponseElement.textContent = "Sensor settings saved successfully!";
                    alert('Sensor settings saved successfully!');
                    resetUnsavedIndicators();
                    fetchConfig();
                } catch (error) {
                    console.error('Error saving sensor settings:', error);
                    deviceResponseElement.textContent = `Error saving sensor settings: ${error.message}`;
                    alert('Error saving sensor settings.');
                }
            }

            async function saveDewHeaterSettings() {
                const dewHeatersConfig = [0, 1].map(i => ({
                    n: originalConfig.dh[i].n, // name
                    en: document.getElementById(`heater-${i}-startup-enabled`).checked, // enabled_on_startup
                    m: parseInt(document.getElementById(`heater-${i}-mode`).value, 10), // mode
                    mp: parseInt(document.getElementById(`heater-${i}-manual-power`).value, 10) || 0, // manual_power
                    to: parseFloat(document.getElementById(`heater-${i}-target-offset`).value.replace(',', '.')), // target_offset
                    kp: parseFloat(document.getElementById(`heater-${i}-pid-kp`).value.replace(',', '.')), // pid_kp
                    ki: parseFloat(document.getElementById(`heater-${i}-pid-ki`).value.replace(',', '.')), // pid_ki
                    kd: parseFloat(document.getElementById(`heater-${i}-pid-kd`).value.replace(',', '.')), // pid_kd
                    sd: parseFloat(document.getElementById(`heater-${i}-start-delta`).value.replace(',', '.')), // start_delta
                    ed: parseFloat(document.getElementById(`heater-${i}-end-delta`).value.replace(',', '.')), // end_delta
                    xp: parseInt(document.getElementById(`heater-${i}-max-power`).value, 10) || 0, // max_power
                    psf: parseFloat(document.getElementById(`heater-${i}-pid-sync-factor`).value.replace(',', '.')) // pid_sync_factor
                }));

                const newConfig = {
                    dh: dewHeatersConfig // dew_heaters
                };

                try {
                    if (connectionIndicator.className !== 'connected') {
                        alert('Cannot save device configuration: Device is not connected.');
                        return;
                    }
                    deviceResponseElement.textContent = "Saving dew heater settings...";
                    const response = await fetch('/api/v1/config/set', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(newConfig)
                    });
                    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                    
                    deviceResponseElement.textContent = "Dew heater settings saved successfully!";
                    alert('Dew heater settings saved successfully!');

                    // After saving heater settings to the device, also save the proxy config
                    // to ensure the "auto-enable leader" setting is persisted.
                    await saveProxyConfig(false); // Pass false to suppress the alert
                    resetUnsavedIndicators();
                    fetchConfig();
                } catch (error) {
                    console.error('Error saving dew heater settings:', error);
                    deviceResponseElement.textContent = `Error saving dew heater settings: ${error.message}`;
                    alert('Error saving dew heater settings.');
                }
            }

            async function saveAdjustableVoltagePreset() {
                const newConfig = {
                    av: parseFloat(document.getElementById('adj-voltage').value.replace(',', '.')) // adj_conv_preset_v
                };

                try {
                    if (connectionIndicator.className !== 'connected') {
                        alert('Cannot save device configuration: Device is not connected.');
                        return;
                    }
                    deviceResponseElement.textContent = "Saving adjustable voltage preset...";
                    const response = await fetch('/api/v1/config/set', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(newConfig)
                    });
                    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                    
                    deviceResponseElement.textContent = "Adjustable voltage preset saved successfully!";
                    alert('Adjustable voltage preset saved successfully!');
                    resetUnsavedIndicators();
                    fetchConfig();
                } catch (error) {
                    console.error('Error saving adjustable voltage preset:', error);
                    deviceResponseElement.textContent = `Error saving adjustable voltage preset: ${error.message}`;
                    alert('Error saving adjustable voltage preset.');
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
                    deviceResponseElement.textContent = "Saving power startup states...";
                    const response = await fetch('/api/v1/config/set', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(newConfig)
                    });
                    if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                    
                    deviceResponseElement.textContent = "Power startup states saved successfully!";
                    alert('Power startup states saved successfully!');
                    resetUnsavedIndicators();
                    fetchConfig();
                } catch (error) {
                    console.error('Error saving power startup states:', error);
                    deviceResponseElement.textContent = `Error saving power startup states: ${error.message}`;
                    alert('Error saving power startup states.');
                }
            }

            async function saveProxyConfig(showAlert = true) {
                const newProxyConfig = { 
                    serialPortName: document.getElementById('proxy-serial-port').value.trim(),
                    autoDetectPort: document.getElementById('proxy-auto-detect-port').checked,
                    networkPort: parseInt(document.getElementById('proxy-network-port').value, 10) || 8080,
                    logLevel: document.getElementById('proxy-log-level').value,
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
                    deviceResponseElement.textContent = "Saving proxy settings...";
                    const response = await fetch('/api/v1/proxy/config/set', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(newProxyConfig)
                    });
                     if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

                    deviceResponseElement.textContent = "Proxy settings saved.";
                    if (showAlert) {
                        alert("Proxy settings saved. Some changes may require an application restart.");
                    }
                    resetUnsavedIndicators();
                    fetchProxyConfig(); // Re-fetch to update originalProxyConfig
                } catch (error) {
                    console.error('Error saving proxy config:', error);
                    deviceResponseElement.textContent = `Error saving proxy config: ${error.message}`;
                    alert('Error saving proxy connection settings.');
                }
            }

            function setupSaveButtons() {
                document.querySelector('#power-startup-states + .save-config-button').addEventListener('click', savePowerStartupStates);
                document.getElementById('adj-voltage').closest('.collapsible-content').querySelector('.save-config-button').addEventListener('click', saveAdjustableVoltagePreset);
                document.querySelector('#dew-heater-settings .collapsible-content > .save-config-button').addEventListener('click', saveDewHeaterSettings);
                document.querySelector('#sensor-settings-group .collapsible-content > .save-config-button').addEventListener('click', saveSensorSettings);
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
                            switchInput.checked = (value === 1);
                            if (value !== 1) allOn = false;
                        }
                    }
                    masterPowerSwitch.checked = allOn;
                } catch (error) {
                    // console.error('Error fetching power status:', error);
                }
            }

            function populatePowerControls(switchNames) {
                const grid = document.getElementById('power-grid');
                const shortSwitchIDMap = {
                    "dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
                    "usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
                };
                grid.innerHTML = '';
                for (const [id, key] of Object.entries(switchIDMap)) {
                    const shortKey = shortSwitchIDMap[key] || key;
                    const displayName = (switchNames && switchNames[key]) ? switchNames[key] : (longToShortKeyMap[key] || key);
                    const controlDiv = document.createElement('div');
                    controlDiv.className = 'switch-control';
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
                    const content = header.nextElementSibling;
                    const icon = header.querySelector('.toggle-icon');
                    const titleElement = header.querySelector('h2, h3');
                    if (!content || !icon || !titleElement) return;
                    const storageKey = `collapsibleState_${titleElement.textContent.trim().replace(/\s+/g, '_')}`;
                    const isCollapsed = localStorage.getItem(storageKey) !== 'false';
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

            async function fetchLiveStatus() {
                let statusOk = false;
                try {
                    const response = await fetch('/api/v1/status');
                    if (!response.ok) throw new Error('No response');
                    const data = await response.json();
                    if (Object.keys(data).length === 0) throw new Error('Empty response');

                    const format = (elId, value, points) => {
                        const el = document.getElementById(elId);
                        if (el) el.textContent = typeof value === 'number' ? value.toFixed(points) : value;
                    }
                    format('status-v', data.v, 2); // v is already short
                    format('status-i', data.i / 1000, 2); // i is already short
                    format('status-p', data.p, 2); // p is already short
                    format('status-t_amb', data.t_amb, 1); // t_amb is already short
                    format('status-h_amb', data.h_amb, 1); // h_amb is already short
                    format('status-d', data.d, 1); // d is already short
                    format('status-t_lens', data.t_lens, 1); // t_lens is already short
                    format('status-pwm1', data.pwm1, 0); // pwm1 is already short
                    format('status-pwm2', data.pwm2, 0); // pwm2 is already short
                    statusOk = true;
                } catch (error) {
                    const fields = ['v', 'i', 'p', 't_amb', 'h_amb', 'd', 't_lens', 'pwm1', 'pwm2'];
                    fields.forEach(f => {
                        const el = document.getElementById(`status-${f}`);
                        if (el) el.textContent = '-';
                    });
                }
                updateConnectionStatus(originalProxyConfig, statusOk);
            }

            function setupLogViewer() {
                const logContainer = document.getElementById('log-container');
                const maxLogLines = 500;
                let socket;
                function connect() {
                    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
                    const host = window.location.host;
                    const wsUrl = `${proto}//${host}/ws/logs`;
                    socket = new WebSocket(wsUrl);
                    socket.onopen = () => {
                        const line = document.createElement('div');
                        line.style.color = '#28a745';
                        line.textContent = '--- Connected to live log stream ---';
                        logContainer.appendChild(line);
                    };
                    socket.onmessage = (event) => {
                        const message = event.data;
                        const line = document.createElement('div');
                        if (message.includes('[ERROR]')) line.className = 'log-line-error';
                        else if (message.includes('[WARN]')) line.className = 'log-line-warn';
                        else if (message.includes('[DEBUG]')) line.className = 'log-line-debug';
                        line.textContent = message.trim();
                        logContainer.appendChild(line);
                        logContainer.scrollTop = logContainer.scrollHeight;
                        while (logContainer.childNodes.length > maxLogLines) {
                            logContainer.removeChild(logContainer.firstChild);
                        }
                    };
                    socket.onclose = () => {
                        const line = document.createElement('div');
                        line.className = 'log-line-error';
                        line.textContent = '--- Disconnected from live log stream. Retrying... ---';
                        logContainer.appendChild(line);
                        logContainer.scrollTop = logContainer.scrollHeight;
                        setTimeout(connect, 3000);
                    };
                    socket.onerror = (error) => console.error('WebSocket error:', error);
                }
                connect();
            }

            async function rebootDevice() {
                if (!confirm('Are you sure you want to reboot the device?')) return;
                try {
                    deviceResponseElement.textContent = "Sending reboot command...";
                    const response = await fetch('/api/v1/command', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ command: 'reboot' })
                    });
                    if (!response.ok) throw new Error(`Server responded with ${response.status}`);
                    deviceResponseElement.textContent = "Reboot command sent successfully. The device will restart.";
                    alert('Reboot command sent. Please wait a moment for the device to restart.');
                } catch (error) {
                    deviceResponseElement.textContent = `Error sending reboot command: ${error.message}`;
                    alert('Error sending reboot command.');
                }
            }

            async function factoryResetDevice() {
                if (!confirm('Are you sure you want to perform a factory reset? This will erase all your settings and reboot the device.')) return;
                try {
                    deviceResponseElement.textContent = "Sending factory reset command...";
                    const response = await fetch('/api/v1/command', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ command: 'factory_reset' })
                    });
                    if (!response.ok) throw new Error(`Server responded with ${response.status}`);
                    deviceResponseElement.textContent = "Factory reset command sent. The device will restart with default settings.";
                    alert('Factory reset command sent. The device will now restart. Please refresh this page after a moment.');
                } catch (error) {
                    deviceResponseElement.textContent = `Error sending factory reset command: ${error.message}`;
                    alert('Error sending factory reset command.');
                }
            }

            // --- Backup & Restore ---
            function createBackup() {
                window.location.href = '/api/v1/backup/create';
            }

            function triggerRestore() {
                restoreFileInput.click();
            }

            async function handleRestoreFile(event) {
                const file = event.target.files[0];
                if (!file) {
                    return;
                }

                if (!confirm('Are you sure you want to restore the configuration? This will overwrite ALL current proxy and device settings.\n\nNOTE: The COM port will be ignored and auto-detection will be triggered to find the device on this computer.')) {
                    event.target.value = null; // Clear the file input
                    return;
                }

                const reader = new FileReader();
                reader.onload = async (e) => {
                    try {
                        const content = e.target.result;
                        JSON.parse(content); // Validate JSON

                        deviceResponseElement.textContent = "Restoring configuration...";
                        const response = await fetch('/api/v1/backup/restore', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: content
                        });

                        if (!response.ok) {
                            const errorText = await response.text();
                            throw new Error(`Restore failed: ${errorText}`);
                        }

                        const successText = await response.text();
                        alert(successText);
                        deviceResponseElement.textContent = successText;

                        // Await the full reload of the config
                        await initialLoad(); 

                    } catch (error) {
                        console.error('Error restoring backup:', error);
                        alert(`Error during restore: ${error.message}`);
                        deviceResponseElement.textContent = `Error during restore: ${error.message}`;
                    } finally {
                        event.target.value = null; // Clear the file input
                    }
                };
                reader.readAsText(file);
            }

            // --- Initial Load & Event Listeners ---
            async function fetchFirmwareVersion() {
                try {
                    const response = await fetch('/api/v1/firmware/version');
                    if (!response.ok) return;
                    const data = await response.json();
                    firmwareVersionElement.textContent = data.version;
                } catch (error) {
                    firmwareVersionElement.textContent = "unknown";
                }
            }

            async function initialLoad() {
                setupCollapsible();
                await fetchProxyConfig(); // Fetches proxy config but doesn't fully apply it yet
                await fetchConfig(); // This function populates the UI, making the checkboxes available
                // Now that the UI is built, we can safely apply the proxy settings to it.
                await fetchPowerStatus();
                await fetchFirmwareVersion();
                setupLogViewer();
                setupChangeDetection();
                setInterval(fetchLiveStatus, 2000); // Fetch status and update connection indicator
                setInterval(fetchPowerStatus, 5000);
            }

            initialLoad();
            
            masterPowerSwitch.addEventListener('change', (e) => setAllPower(e.target.checked));
            document.getElementById('heater-0-mode').addEventListener('change', () => updateHeaterModeView(0));
            document.getElementById('heater-1-mode').addEventListener('change', () => updateHeaterModeView(1));

            document.getElementById('proxy-auto-detect-port').addEventListener('change', (e) => {
                const serialPortInput = document.getElementById('proxy-serial-port');
                serialPortInput.disabled = e.target.checked;
            });

            document.getElementById('save-proxy-config-button').addEventListener('click', () => saveProxyConfig(true));
            document.getElementById('save-switch-names-button').addEventListener('click', saveProxyConfig);
            if (rebootButton) rebootButton.addEventListener('click', rebootDevice);
            if (factoryResetButton) factoryResetButton.addEventListener('click', factoryResetDevice);
            if (backupButton) backupButton.addEventListener('click', createBackup);
            if (restoreButton) restoreButton.addEventListener('click', triggerRestore);
            if (restoreFileInput) restoreFileInput.addEventListener('change', handleRestoreFile);

            setupSaveButtons();
        });
    