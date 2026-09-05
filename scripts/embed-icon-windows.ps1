[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Executable,

    [string]$Icon = (Join-Path $PSScriptRoot '..\internal\winui\assets\ctyunhelper.ico')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$exePath = (Resolve-Path $Executable).Path
$iconPath = (Resolve-Path $Icon).Path
$iconBytes = [System.IO.File]::ReadAllBytes($iconPath)

if ($iconBytes.Length -lt 22 -or [BitConverter]::ToUInt16($iconBytes, 0) -ne 0 -or [BitConverter]::ToUInt16($iconBytes, 2) -ne 1) {
    throw "Invalid ICO file: $iconPath"
}

$count = [BitConverter]::ToUInt16($iconBytes, 4)
if ($count -lt 1 -or $iconBytes.Length -lt (6 + 16 * $count)) {
    throw "Invalid ICO directory: $iconPath"
}

if (-not ('CtyunHelper.WinResourceNative' -as [type])) {
    Add-Type @'
using System;
using System.Runtime.InteropServices;
namespace CtyunHelper {
    public static class WinResourceNative {
        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern IntPtr BeginUpdateResource(string fileName, bool deleteExistingResources);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern bool UpdateResource(IntPtr update, IntPtr type, IntPtr name, ushort language, byte[] data, uint size);

        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool EndUpdateResource(IntPtr update, bool discard);
    }
}
'@
}

# Windows stores icon images as RT_ICON entries plus one RT_GROUP_ICON directory.
# Reuse the PNG/DIB payloads from the ICO so no external resource compiler is needed.
$group = New-Object 'System.Byte[]' (6 + 14 * $count)
[BitConverter]::GetBytes([uint16]0).CopyTo($group, 0)
[BitConverter]::GetBytes([uint16]1).CopyTo($group, 2)
[BitConverter]::GetBytes([uint16]$count).CopyTo($group, 4)

$update = [CtyunHelper.WinResourceNative]::BeginUpdateResource($exePath, $false)
if ($update -eq [IntPtr]::Zero) {
    throw "BeginUpdateResource failed. Win32=$([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
}

$commit = $false
try {
    for ($i = 0; $i -lt $count; $i++) {
        $icoOffset = 6 + 16 * $i
        $dataSize = [BitConverter]::ToUInt32($iconBytes, $icoOffset + 8)
        $dataOffset = [BitConverter]::ToUInt32($iconBytes, $icoOffset + 12)
        if ($dataSize -eq 0 -or ($dataOffset + $dataSize) -gt $iconBytes.Length) {
            throw "ICO image $i is out of bounds"
        }

        $image = New-Object 'System.Byte[]' $dataSize
        [Buffer]::BlockCopy($iconBytes, [int]$dataOffset, $image, 0, [int]$dataSize)
        $resourceId = [uint16]($i + 1)
        if (-not [CtyunHelper.WinResourceNative]::UpdateResource($update, [IntPtr]3, [IntPtr]$resourceId, 0, $image, $dataSize)) {
            throw "Writing RT_ICON $resourceId failed. Win32=$([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
        }

        $groupOffset = 6 + 14 * $i
        $group[$groupOffset] = $iconBytes[$icoOffset]
        $group[$groupOffset + 1] = $iconBytes[$icoOffset + 1]
        $group[$groupOffset + 2] = $iconBytes[$icoOffset + 2]
        $group[$groupOffset + 3] = $iconBytes[$icoOffset + 3]
        [BitConverter]::GetBytes([BitConverter]::ToUInt16($iconBytes, $icoOffset + 4)).CopyTo($group, $groupOffset + 4)
        [BitConverter]::GetBytes([BitConverter]::ToUInt16($iconBytes, $icoOffset + 6)).CopyTo($group, $groupOffset + 6)
        [BitConverter]::GetBytes([uint32]$dataSize).CopyTo($group, $groupOffset + 8)
        [BitConverter]::GetBytes($resourceId).CopyTo($group, $groupOffset + 12)
    }

    if (-not [CtyunHelper.WinResourceNative]::UpdateResource($update, [IntPtr]14, [IntPtr]1, 0, $group, $group.Length)) {
        throw "Writing RT_GROUP_ICON failed. Win32=$([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
    }
    $commit = $true
}
finally {
    if (-not [CtyunHelper.WinResourceNative]::EndUpdateResource($update, -not $commit)) {
        throw "EndUpdateResource failed. Win32=$([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
    }
}

Write-Host "Embedded Windows application icon: $exePath"
