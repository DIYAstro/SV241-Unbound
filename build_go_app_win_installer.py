import os
import json
import subprocess
Import("env")

def build_go_proxy(source, target, env):
    """Compiles the Go proxy with version info."""
    print("--- Building Go proxy ---")
    proxy_dir = os.path.join(env.subst("$PROJECT_DIR"), "AscomAlpacaProxy")
    go_bin_path = os.path.join(os.path.expanduser("~"), "go", "bin")
    goversioninfo_path = os.path.join(go_bin_path, "goversioninfo")
    resource_path = os.path.join(proxy_dir, "resource.syso")
    version_info_path = os.path.join(proxy_dir, "versioninfo.json")

    # 1. Read version from versioninfo.json to inject into the binary
    try:
        with open(version_info_path, "r") as f:
            version_info = json.load(f)
        product_version = version_info["StringFileInfo"]["ProductVersion"]
        print(f"--> Found ProductVersion for Go build: {product_version}")
    except Exception as e:
        print(f"Error reading version info for Go build: {e}")
        env.Exit(1)

    # Commands
    install_cmd = "go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest"
    versioninfo_cmd = f'"{goversioninfo_path}" -o {resource_path} {version_info_path}'
    ldflags = f'-ldflags="-H=windowsgui -X main.AppVersion={product_version}"'
    build_cmd = f'go build {ldflags} -o build/AscomAlpacaProxy.exe .'

    try:
        # Install goversioninfo
        print("--> Installing goversioninfo...")
        subprocess.run(install_cmd, shell=True, check=True, capture_output=True)

        # Create resource file
        print("--> Creating version resource file...")
        subprocess.run(versioninfo_cmd, shell=True, check=True, capture_output=True)

        # Build Go app
        print(f"--> Running go build in {proxy_dir}")
        build_result = subprocess.run(build_cmd, cwd=proxy_dir, shell=True, check=True, capture_output=True, text=True)
        print("--> Go build successful.")

    except subprocess.CalledProcessError as e:
        print("\n!!! Error during Go build process !!!")
        print(f"Command failed: {e.cmd}")
        print(f"Return Code: {e.returncode}")
        print(f"STDOUT:\n{e.stdout}")
        print(f"STDERR:\n{e.stderr}")
        env.Exit(1)
    finally:
        # Clean up resource file
        if os.path.exists(resource_path):
            os.remove(resource_path)
            print(f"--> Cleaned up resource file.")

def create_installer(source, target, env):
    """Creates the Inno Setup installer."""
    print("--- Creating Windows installer ---")

    proxy_dir = os.path.join(env.subst("$PROJECT_DIR"), "AscomAlpacaProxy")
    version_info_path = os.path.join(proxy_dir, "versioninfo.json")
    installer_template_path = os.path.join(proxy_dir, "installer.iss")
    temp_installer_script_path = os.path.join(proxy_dir, "temp_installer.iss")

    # 1. Read version from versioninfo.json
    try:
        with open(version_info_path, "r") as f:
            version_info = json.load(f)
        
        # Extract ProductVersion for the filename
        product_version = version_info["StringFileInfo"]["ProductVersion"]
        print(f"--> Found ProductVersion: {product_version}")

        # Extract LegalCopyright
        copyright_info = version_info["StringFileInfo"]["LegalCopyright"]
        print(f"--> Found Copyright: {copyright_info}")

        # Construct FileVersion string (e.g., "1.2.3.4")
        fv = version_info["FixedFileInfo"]["FileVersion"]
        file_version = f"{fv['Major']}.{fv['Minor']}.{fv['Patch']}.{fv['Build']}"
        print(f"--> Found FileVersion: {file_version}")

    except Exception as e:
        print(f"Error reading version info: {e}")
        env.Exit(1)

    # 2. Create installer script from template
    try:
        with open(installer_template_path, "r") as f:
            installer_script = f.read()
        
        installer_script = installer_script.replace("##VERSION##", product_version)
        installer_script = installer_script.replace("##FILEVERSION##", file_version)
        installer_script = installer_script.replace("##COPYRIGHT##", copyright_info)
        
        with open(temp_installer_script_path, "w") as f:
            f.write(installer_script)
        print("--> Generated temporary Inno Setup script.")
    except Exception as e:
        print(f"Error creating installer script: {e}")
        env.Exit(1)

    # 3. Find Inno Setup Compiler
    inno_setup_path = "C:\\Program Files (x86)\\Inno Setup 6\\iscc.exe"
    if not os.path.exists(inno_setup_path):
        print(f"Error: Inno Setup compiler not found at '{inno_setup_path}'")
        print("Please install Inno Setup 6 or adjust the path in the script.")
        os.remove(temp_installer_script_path)
        env.Exit(1)

    # 4. Compile the installer
    print(f"--> Running Inno Setup compiler...")
    try:
        result = subprocess.run(
            [inno_setup_path, os.path.basename(temp_installer_script_path)],
            cwd=proxy_dir,
            check=True,
            capture_output=True,
            text=True
        )
        print("--> Inno Setup compilation successful.")
    except subprocess.CalledProcessError as e:
        print("\n!!! Error during Inno Setup compilation !!!")
        print(e.stdout)
        print(e.stderr)
        env.Exit(1)
    finally:
        # 5. Clean up temporary file
        if os.path.exists(temp_installer_script_path):
            os.remove(temp_installer_script_path)
            print("--> Cleaned up temporary script.")

    print("--- Installer created successfully! ---")

def build_and_create_installer(source, target, env):
    """A single function to run both build steps in order."""
    build_go_proxy(source, target, env)
    create_installer(source, target, env)

env.AddCustomTarget(
    "buildgoproxywininstaller",
    None,
    build_and_create_installer,
    "Build Go Proxy + Windows Installer",
    "Builds the Go proxy and creates a Windows installer"
)
