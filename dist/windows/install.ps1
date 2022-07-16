<#
  .SYNOPSIS
  Installs OVS, OVN and Kube-OVN.
  .PARAMETER KubernetesPath
  Directory that contains Kubernetes CLI. The default value is "C:\k".
  .PARAMETER CniBinDir
  Path of the directory containing CNI binaries. The default value is "/opt/cni/bin".
  .PARAMETER KubeConfig
  Path of the kubeconfig used to install Kube-OVN.
  .PARAMETER ApiServer
  URL of the Kubernetes API Server, e.g. https://192.168.0.1:6443.
  .PARAMETER ServiceCIDR
  Service CIDR of the Kubernetes Cluster.
  .PARAMETER NodeName
  Kubernetes node name of this host. The default value is the hostname.
  .PARAMETER Namespace
  Namespace in which the Kube-OVN is installed. The default value is "kube-system".
  .PARAMETER ServiceAccount
  Kubernetes ServiceAccount used by Kube-OVN. The default value is "ovn".
  .PARAMETER OvnDbPort
  Port of the OVN SB database. The default value is 6642.
  .PARAMETER TunnelType
  Tunnnel encapsulation type. The default value is "geneve".
  .PARAMETER EnableMirror
  Enable traffic mirror. The default value is false.
#>
Param(
    [parameter(Mandatory = $false)] [string] $KubernetesPath = "C:\k",
    [parameter(Mandatory = $false)] [string] $CniBinDir = "/opt/cni/bin",
    [parameter(Mandatory = $true)] [string] $KubeConfig,
    [parameter(Mandatory = $true)] [string] $ApiServer,
    [parameter(Mandatory = $true)] [string] $ServiceCIDR,
    [parameter(Mandatory = $false)] [string] $NodeName = $(hostname).ToLower(),
    [parameter(Mandatory = $false)] [string] $Namespace = "kube-system",
    [parameter(Mandatory = $false)] [string] $ServiceAccount = "ovn",
    [parameter(Mandatory = $false)] [ValidateRange(1, 65535)] [int] $OvnDbPort = 6642,
    [parameter(Mandatory = $false)] [ValidateSet("geneve","vxlan","stt")] [string] $TunnelType = "geneve",
    [parameter(Mandatory = $false)] [switch] $EnableMirror = $false
)

$ErrorActionPreference = 'Stop'

$Powershell = (Get-Command powershell).Source
$PowershellArgs = "-ExecutionPolicy Bypass -NoProfile"
$ovnBinPath = "C:\ovn\usr\bin"

function SetConfig([string]$Path, [hashtable]$NewConfig) {
    $global:kvs = @{}
    Get-Content $Path | ForEach-Object -process {
        $k = [regex]::split($_, '=')
        if ($k.Length -ne 2) {
            return
        }
        $k[0] = $k[0].Trim()
        $k[1] = $k[1].Trim()
        if (($k[0].CompareTo("") -ne 0)) {
            $global:kvs[$k[0]] = $k[1]
        }
    }

    foreach ($k in $NewConfig.keys) {
        $global:kvs[$k] = $NewConfig.$k
    }

    $flag = $false
    foreach ($k in $global:kvs.keys) {
        if ($flag) {
            Add-Content -Encoding utf8 $Path ("{0} = {1}" -f $k, $global:kvs.$k)
        } else {
            Set-Content -Encoding utf8 $Path ("{0} = {1}" -f $k, $global:kvs.$k)
            $flag = $true
        }
    }
}

function RegisterService([hashtable]$Params) {
    if ((Get-Service | Where-Object Name -eq $Params.Name)) {
        [string]::Format("Service {0} already exists", $Params.Name) | Write-Host
        return
    }
    New-Service @Params
}

# check whether the ovs-vswitchd is running
$vswitchd = Get-Service | Where-Object Name -eq "ovs-vswitchd"
if (!$vswitchd) {
    Write-Host "Service ovs-vswitchd is not installed, please install Open vSwitch first!"
    exit 1
}
if ($vswitchd.Status -ne "Running") {
    Write-Host "Service ovs-vswitchd is not running, please check the Open vSwitch installation first!"
    exit 1
}

Push-Location "$PSScriptRoot"

if (!$env:Path.Split(";").Contains($ovnBinPath)) {
    Write-Host "Adding OVN bin to PATH"
    $newPath = [string]::Format("{0};{1}", $env:Path, $ovnBinPath)
    [Environment]::SetEnvironmentVariable("Path", $newPath, [System.EnvironmentVariableTarget]::Machine)
}

$env:Path += ";$KubernetesPath"

Write-Host "Retrieving ovn-sb addresses"
$ovnDbIPs = $(kubectl --kubeconfig=$KubeConfig -n $Namespace get pod -l app=ovn-central -o jsonpath='{.items[*].status.podIP}').Replace(' ', ',')

Write-Host "Retrieving Kubernetes ServiceAccount token"
$token = $(kubectl --kubeconfig=$KubeConfig get secret -n $Namespace -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='$ServiceAccount')].data.token}")
$token = $([System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($token)))

Write-Host "Installing OVN"
Copy-Item -Force -Path "ovn" -Destination "/" -Recurse
foreach ($dir in @("/ovn/var/log", "/ovn/var/run/ovn")) {
    if (!(Test-Path -Path $dir)) {
        New-Item -ItemType directory -Path $dir | Out-Null
    }
}

$systemIdConf = "/ovn/etc/system-id.conf"
$systemId = ""
if (Test-Path -Path $systemIdConf) {
    $systemId = $(Get-Content -Path $systemIdConf).Trim()
} else {
    $systemId = New-Guid | Select -ExpandProperty "Guid"
    Out-File -Encoding utf8 -InputObject $systemId -FilePath $systemIdConf
}

Write-Host "Installing Kube-OVN"
Copy-Item -Force -Path "kube-ovn" -Destination "/" -Recurse
Copy-Item -Force -Path "kube-ovn/bin/kube-ovn.exe" "$CniBinDir"
if (!(Test-Path -Path "/kube-ovn/log")) {
    New-Item -ItemType directory -Path "/kube-ovn/log" | Out-Null
}

Pop-Location

Write-Host "Updating ovn-controller configuration"
$cfg = @{
    "OVN_DB_IPS" = $ovnDbIPs
    "OVN_DB_PORT" = $OvnDbPort
    "NODE_NAME" = $NodeName
    "TUNNEL_TYPE" = $TunnelType
}
SetConfig "/ovn/etc/ovn-controller.conf" $cfg

Write-Host "Updating kube-ovn configuration"
$cfg = @{
    "SVC_CIDR" = $ServiceCIDR
    "NODE_NAME" = $NodeName
    "ENABLE_MIRROR" = $EnableMirror
}
SetConfig "/kube-ovn/etc/kube-ovn.conf" $cfg

Write-Host "Generating kubeconfig for Kube-OVN"
$ovnKubeConfig = "/kube-ovn/etc/kube-ovn.kubeconf"
kubectl config --kubeconfig=$ovnKubeConfig set-cluster kubernetes --server=$ApiServer --insecure-skip-tls-verify
kubectl config --kubeconfig=$ovnKubeConfig set-credentials $ServiceAccount --token=$token
kubectl config --kubeconfig=$ovnKubeConfig set-context $ServiceAccount@kubernetes --cluster=kubernetes --user=$ServiceAccount
kubectl config --kubeconfig=$ovnKubeConfig use-context $ServiceAccount@kubernetes

Write-Host "Registering kube-ovn service"
nssm install "kube-ovn" $Powershell $PowershellArgs "C:\kube-ovn\bin\start-kube-ovn.ps1"
nssm set "kube-ovn" DependOnService "ovs-vswitchd"
sc.exe config "kube-ovn" start= delayed-auto

Write-Host "Starting kube-ovn service"
Start-Service kube-ovn

# wait hns network to be ready
$hnsNetwork = "kube-ovn"
Write-Host "Waiting for HNS network $hnsNetwork to be ready..."
$ready = $false
DO {
    $network = Get-HnsNetwork | Where-Object {$_.Name -eq $hnsNetwork}
    if ($network -and $network.Type -eq "transparent") {
        $ovsExt = $network.Extensions | Where-Object {$_.Id -eq "583CC151-73EC-4A6A-8B47-578297AD7623"}
        if ($ovsExt -and $ovsExt.IsEnabled) {
            $ready = $true
        }
    }
    if (!$ready) {
        Write-Host "HNS network $hnsNetwork is not ready, wait 3 seconds..."
        Start-Sleep -s 3
    }
} Until ($ready)

Write-Host "Registering ovn-controller service"
nssm install "ovn-controller" $Powershell $PowershellArgs "C:\ovn\start-ovn-controller.ps1"
nssm set "ovn-controller" DependOnService "ovs-vswitchd"
sc.exe config "ovn-controller" start= delayed-auto

Write-Host "Starting ovn-controller service"
Start-Service ovn-controller

Write-Host "Kube-OVN installation finished, enjoy it!"
