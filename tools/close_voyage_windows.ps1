# close_voyage_windows.ps1 — 按「前哨 2.0」标题枚举关闭前哨 App 窗口
# 理由：Edge App 窗口无独立进程（挂在 msedge --no-startup-window 主进程下），
#      按进程命令行匹配永远关不掉旧窗（v2 缺陷）。改走 Win32 窗口标题枚举。
Add-Type @"
using System;
using System.Text;
using System.Runtime.InteropServices;
public class WinKill {
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc cb, IntPtr lp);
  [DllImport("user32.dll")] public static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  public delegate bool EnumWindowsProc(IntPtr h, IntPtr lp);
}
"@
$cb = [WinKill+EnumWindowsProc]{
  param($h, $lp)
  if ([WinKill]::IsWindowVisible($h)) {
    $sb = New-Object System.Text.StringBuilder 128
    [WinKill]::GetWindowText($h, $sb, 128) | Out-Null
    if ($sb.ToString() -like '*前哨 2.0*') {
      [WinKill]::PostMessage($h, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null  # WM_CLOSE
    }
  }
  return $true
}
[WinKill]::EnumWindows($cb, [IntPtr]::Zero) | Out-Null
