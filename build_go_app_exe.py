import os
import re
import json
Import("env")

go_bin_path = os.path.join(os.path.expanduser("~"), "go", "bin")
goversioninfo_path = os.path.join(go_bin_path, "goversioninfo")

def extract_firmware_version():
    """Extract FIRMWARE_VERSION from config_manager.h and write to version.json"""
    config_manager_path = "src/config_manager.h"
    version_json_path = "AscomAlpacaProxy/frontend-vue/dist/flasher/firmware/version.json"
    
    try:
        with open(config_manager_path, "r", encoding="utf-8") as f:
            content = f.read()
        
        # Extract version using regex: #define FIRMWARE_VERSION "x.y.z"
        match = re.search(r'#define\s+FIRMWARE_VERSION\s+"([^"]+)"', content)
        if match:
            version = match.group(1)
            print(f"Extracted firmware version: {version}")
            
            # Write to version.json
            version_data = {"version": version}
            with open(version_json_path, "w", encoding="utf-8") as f:
                json.dump(version_data, f, indent=4)
            print(f"Updated {version_json_path}")
        else:
            print("Warning: Could not find FIRMWARE_VERSION in config_manager.h")
    except Exception as e:
        print(f"Error extracting firmware version: {e}")

# Extract firmware version before build
extract_firmware_version()

env.AddCustomTarget(
    "buildgoproxyexe",
    None,
    [
        "go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest",
        f'"{goversioninfo_path}" -o AscomAlpacaProxy/resource.syso AscomAlpacaProxy/versioninfo.json',
        "cd AscomAlpacaProxy && go build -ldflags \"-H=windowsgui\" -o build/AscomAlpacaProxy.exe .",
        "del AscomAlpacaProxy\\resource.syso"
    ],
    "Build Go Proxy Windows Executable",
    "Builds the Go proxy application for Windows"
)
