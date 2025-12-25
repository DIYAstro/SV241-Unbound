<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'

const logContainer = ref(null)
const logs = ref([]) // Array of objects { id, text, type }
const isCollapsed = ref(true) // Default to collapsed
let socket = null
let reconnectTimer = null
let nextId = 0
const MAX_LOGS = 500

onMounted(() => {
    connectWebSocket()
    const savedState = localStorage.getItem('collapsed-live-log-viewer')
    if (savedState === 'false') {
        isCollapsed.value = false
    }
})

onUnmounted(() => {
    if (socket) socket.close()
    if (reconnectTimer) clearTimeout(reconnectTimer)
})

function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/logs`;
    
    socket = new WebSocket(wsUrl);

    socket.onopen = () => {
        addLog("Log WebSocket Connected", "info")
    };

    socket.onmessage = (event) => {
        const message = event.data;
        if (!message.trim()) return;
        
        // Type detection
        let type = 'normal';
        if (message.includes('ERROR')) type = 'error';
        else if (message.includes('WARN')) type = 'warn';
        else if (message.includes('DEBUG')) type = 'debug';
        
        // Add directly as single log line (matches legacy behavior)
        logs.value.push({
            id: nextId++,
            text: message,
            type: type
        });
        
        if (logs.value.length > MAX_LOGS) {
            logs.value = logs.value.slice(-MAX_LOGS);
        }
        scrollToBottom();
    };

    socket.onclose = () => {
        addLog("Log WebSocket Disconnected. Reconnecting in 3s...", "error")
        reconnectTimer = setTimeout(connectWebSocket, 3000);
    };

    socket.onerror = (error) => {
        console.error("WebSocket Error:", error);
        socket.close();
    };
}

function addLog(text, type = 'normal') {
    // Check if we need to split lines if multiple were sent
    const lines = text.split('\n');
    lines.forEach(line => {
        if (!line.trim()) return;
        logs.value.push({
            id: nextId++,
            text: line,
            type: type
        });
    });

    if (logs.value.length > MAX_LOGS) {
        logs.value = logs.value.slice(-MAX_LOGS);
    }
    scrollToBottom();
}

function scrollToBottom() {
    nextTick(() => {
        if (logContainer.value) {
            logContainer.value.scrollTop = logContainer.value.scrollHeight
        }
    })
}

function downloadLog() {
    window.location.href = '/api/v1/log/download';
}

function toggleCollapse() {
    isCollapsed.value = !isCollapsed.value
    localStorage.setItem('collapsed-live-log-viewer', isCollapsed.value)
}
</script>

<template>
  <div class="glass-panel card full-width" id="live-log-viewer">
      <div class="collapsible-header" @click="toggleCollapse">
          <h2>Live Log</h2>
          <span class="toggle-icon" :class="{ collapsed: isCollapsed }"></span>
      </div>
      
      <div class="collapsible-content" :class="{ collapsed: isCollapsed }">
          <div ref="logContainer" class="log-container">
              <div v-for="log in logs" :key="log.id" :class="['log-line', 'log-' + log.type]">
                  {{ log.text }}
              </div>
          </div>
          <button @click="downloadLog" class="btn-secondary" style="margin-top: 0.5rem; padding: 0.4rem 0.8rem;">Download Log</button>
      </div>
  </div>
</template>

<style scoped>
.log-container {
    background: rgba(0, 0, 0, 0.4);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    padding: 0.5rem;
    height: 300px;
    overflow-y: auto;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 0.85rem;
    color: #e0e0e0;
    
    /* Scrollbar styling for Webkit */
    scrollbar-width: thin;
    scrollbar-color: rgba(255, 255, 255, 0.3) rgba(0, 0, 0, 0.2);
}

.log-line {
    padding: 1px 0;
    white-space: pre-wrap;
    word-break: break-all;
}

.log-error { color: #ff6b6b; }
.log-warn { color: #feca57; }
.log-debug { color: #54a0ff; }

.log-container::-webkit-scrollbar {
    width: 8px;
}
.log-container::-webkit-scrollbar-track {
    background: rgba(0, 0, 0, 0.2);
    border-radius: 0 6px 6px 0;
}
.log-container::-webkit-scrollbar-thumb {
    background-color: rgba(255, 255, 255, 0.3);
    border-radius: 4px;
}

.collapsible-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    cursor: pointer;
    user-select: none;
    padding: 0.5rem 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    margin-bottom: 1rem;
}

.collapsible-header:hover .toggle-icon {
    border-color: #fff;
}

.collapsible-header h2 {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 500;
}
</style>
