@echo off
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo Go is not installed or not in PATH.
  echo Build clash-jump-manager.exe on a machine with Go, then run the exe directly.
  pause
  exit /b 1
)

go run ./cmd/clash-jump-manager
