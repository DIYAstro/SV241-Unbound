# Troubleshooting Guide

## Proxy Issues

### "Serial port busy" Error

**Symptoms:**
- Auto-detection fails with "Serial port busy"
- Log shows: `Could not open port COMX to probe: Serial port busy`

**Solutions:**

1. **Reboot your computer**  
   This ensures all port handles are properly released.

2. **Don't run as Admin**  
   The proxy doesn't require admin rights. Running as admin may cause permission issues with config files. If you did run as admin, try:
   - Delete the config folder at `%APPDATA%\SV241-AscomAlpacaProxy`
   - Start fresh as a normal user

3. **Check for multiple proxy instances**  
   Open Task Manager → search for "AscomAlpacaProxy" → end all instances → start fresh.

4. **Manually configure the port**  
   If auto-detect keeps failing:
   - Open Setup Page → **System** tab
   - Enter your port (e.g., `COM3` - check Device Manager for the correct one)
   - **Disable** "Auto-detect Port"
   - Click **Save**

5. **Check for other software**  
   Make sure no other apps are using the port (serial monitors, other astronomy software, etc.)

6. **Edit config file directly**  
   If the Setup page is not accessible, you can [manually configure the port](./AscomAlpacaProxy/readme.md#manual-configuration-proxy_configjson):
   1. Close the proxy completely
   2. Navigate to `%APPDATA%\SV241-AscomAlpacaProxy`
   3. Edit `proxy_config.json` and set:
      ```json
      "serialPortName": "COM3",
      "autoDetectPort": false
      ```
      (Replace `COM3` with your actual port from Device Manager)
   4. Save and restart the proxy

---

## Firmware Issues

### Web Flasher not working

**Requirements:**
- Use **Chrome** or **Edge** browser (Web Serial API required)
- Firefox and Safari are not supported

**If flashing fails:**
1. Close any other applications that might be using the serial port
2. Disconnect and reconnect the USB cable
3. Try a different USB port (preferably directly on the computer, not a hub)

---

*For additional help, please [open an issue on GitHub](https://github.com/DIYAstro/SV241-Unbound/issues).*
