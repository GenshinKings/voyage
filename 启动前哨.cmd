@echo off
rem Voyage launcher v2: fresh page on every launch (close stale app window, reopen with version param)
cd /d "%~dp0"
netstat -ano | findstr /C:"127.0.0.1:7412" | findstr /C:"LISTENING" >nul 2>&1
if not errorlevel 1 goto reuse
start "voyage" /min outpost2.exe serve --no-open
setlocal enabledelayedexpansion
set /a tries=0
:waitloop
ping -n 2 127.0.0.1 >nul
curl -s -o nul http://127.0.0.1:7412/api/state
if errorlevel 1 (
  set /a tries+=1
  if !tries! LSS 15 goto waitloop
)
endlocal
:reuse
rem close the stale voyage app window (matched by its unique --app command line), then reopen fresh
powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \"Name='msedge.exe'\" | Where-Object { $_.CommandLine -like '*--app=*127.0.0.1:7412*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -EA SilentlyContinue }"
ping -n 2 127.0.0.1 >nul
set "EDGE=%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe"
if not exist "%EDGE%" set "EDGE=%ProgramFiles%\Google\Chrome\Application\chrome.exe"
start "" "%EDGE%" --app=http://127.0.0.1:7412
exit /b 0
