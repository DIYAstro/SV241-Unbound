# Driver Installation

[← Back to proxy overview](./ASCOM_PROXY.md)

To connect your astronomy software to this proxy, a driver must be registered within the ASCOM system. This step is necessary if your software cannot connect to the Alpaca device directly, which can happen for two main reasons:

1.  The software does not have a built-in Alpaca client and relies on the classic ASCOM Chooser.
2.  The software has an Alpaca client, but the automatic network discovery process is not working (e.g., due to firewall settings or network configuration).

Registering a driver solves this by creating a permanent entry in the ASCOM Chooser. This entry acts as a bridge, telling the system exactly how to find and communicate with the proxy.

We provide two methods for this registration: an easy, automated script and a manual method.

## Easy Driver Creation (Recommended)

The installer includes a helper script that automates the entire driver creation process. This is the recommended method as it is fast, easy, and avoids common configuration errors.

1.  **Open the Start Menu**
    *   Navigate to the program folder (usually named `SV241-Unbound`).

2.  **Run the Helper Script**
    *   Click on **"Create SV241 Ascom Driver"**.
    *   This will open a new window and launch an interactive script.

3.  **Follow the On-Screen Instructions**
    *   The script will ask you to select the driver type (`Switch` or `ObservingConditions`). The default is `Switch`.
    *   It will ask you to provide a name for the driver. A default name will be suggested.
    *   It will automatically detect the correct network port from your proxy configuration and suggest it as the default.
    *   Simply press **Enter** at each prompt to accept the defaults, which is sufficient in most cases.

4.  **Done**
    *   After a few moments, the script will confirm that the driver has been successfully created.

**Result:** The driver is now registered system-wide. You can now open your astronomy software and select the driver you just created (e.g., "SV241 Power Switch") directly from the device list.

> **Note:** You can run this script multiple times to create drivers with different names or to create both a `Switch` and an `ObservingConditions` driver.

---

## Manual Driver Creation (Fallback)

If you prefer to set up the driver manually, or if the helper script fails for any reason, you can use the "ASCOM Diagnostics" application that comes with the ASCOM Platform.

1.  **Start ASCOM Diagnostics**

    Open the "ASCOM Diagnostics" application.
    *   You can find it in the Windows Start Menu under the "ASCOM Platform" folder.

2.  **Open the "Switch Chooser"**

    *   In the main window of ASCOM Diagnostics, select the device type `Switch` from the "Select Device Type" dropdown list.
    *   Click the `Choose Device...` button next to it.

3.  **Create a New Alpaca Driver**

    The "ASCOM Switch Chooser" window will open.
    *   In the menu bar of this window, click on `Alpaca`.
    *   Select `Create Alpaca Driver (Admin)` from the dropdown menu.
    *   Windows (UAC) may ask for administrative rights. Please confirm this.

4.  **Name the Driver**

    A small dialog box will ask for a name.
    *   Enter a descriptive name, e.g., `My Manual SV241 Switch`
    *   Click `OK`.

5.  **Configure the Alpaca Connection (Most Important Step)**

    You are now back in the "ASCOM Switch Chooser". Your new driver (e.g., `Switch.My_Manual_SV241_Switch`) is now highlighted in the list.
    *   Click the `Properties...` button.
    *   A setup window will open. Enter the exact connection details here:
        *   **Remote Device Host Name or IP Address:** `localhost`
        *   **Alpaca Port:** `32241` (or the port your proxy is running on)
        *   **Remote Device Number:** `0` (default for the first device)
    *   Click `OK` in the setup window.

6.  **Finalize Selection**

    *   Click `OK` in the "ASCOM Switch Chooser" window as well.

7.  **Test (Optional, but Recommended)**

    *   You are now back in the main window of ASCOM Diagnostics. The name of your new driver is now in the text field.
    *   Click `Connect`.
    *   If everything is configured correctly, the connection should be established successfully (the fields in the "Capabilities" section will be populated).

**Result:** Your manually configured driver "My Manual SV241 Switch" is now permanently registered in the ASCOM system. When you now start NINA (or other software), you can select it directly from the device list without having to enter the IP address again.

> **Note:** Repeat this process for the `ObservingConditions` device to also add the environmental sensors manually.
