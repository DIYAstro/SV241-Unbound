import { computed } from 'vue'

// Warning text shown when a heater in an automatic lens-probe mode (1 = PID, 4 = Min Temp) cannot
// regulate. The firmware keeps such a heater off (safety check in dew_control.cpp), so this must be
// obvious. Returns one entry per heater: '' (all fine) or a short reason.
//
// config: firmware config ({ dh: [{m}], lp: [{id, h}] }), liveStatus: /api/v1/status data
// ({ t_lens, t_lens2 }, null = no valid reading), isConnected: ref/boolean.
export function useLensProbeWarnings(config, liveStatus, isConnected) {
    return computed(() => {
        const cfg = config.value || {}
        const dh = cfg.dh || []
        const lp = cfg.lp || []
        const live = liveStatus.value || {}
        const keys = ['t_lens', 't_lens2']
        return [0, 1].map(h => {
            const mode = dh[h] ? dh[h].m : null
            if (mode !== 1 && mode !== 4) return ''
            const assigned = lp.some(p => p.h === h)
            if (!assigned) return 'No lens probe assigned'
            // Assigned, but the probe delivers no value (unplugged / not found / unreadable).
            if (isConnected.value && live[keys[h]] == null) return 'Assigned lens probe not found'
            return ''
        })
    })
}

export const LENS_PROBE_WARNING_SUFFIX = 'Heater stays off until a lens probe is assigned'
