@echo off
title NKB Diskless System - Windows 11 Golden Image Builder
echo =================================================================
echo   NKB DISKLESS SYSTEM - WINDOWS 11 GOLDEN IMAGE BUILDER
echo =================================================================
echo Requesting administrator privileges...

powershell.exe -NoExit -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\deploy-windows-image.ps1"

pause
