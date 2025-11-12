@echo off
:: This batch file executes the PowerShell script to create an ASCOM Alpaca driver.
:: It handles two common issues:
:: 1. It requests Administrator privileges, which are required to register the driver.
:: 2. It bypasses the PowerShell execution policy for this session only.

:: Get the directory of the batch file itself
SET "SCRIPT_DIR=%~dp0"

:: Execute the PowerShell script with elevated privileges and the correct policy
powershell.exe -ExecutionPolicy Bypass -Command "& {Start-Process PowerShell -ArgumentList '-ExecutionPolicy Bypass -File ""%SCRIPT_DIR%Create-AscomDriver.ps1""' -Verb RunAs}"

echo.
echo The script has been launched in a new Administrator window.
pause