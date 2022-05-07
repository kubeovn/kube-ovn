$ErrorActionPreference = 'Stop'

$configPath = "C:\kube-ovn\etc\kube-ovn.conf"
if (!(Test-Path -Path $configPath)) {
    Write-Host "Error: configuration file $configPath is missing!"
    exit 1
}

$global:kubeovnConfig = @{}
Get-Content $configPath | ForEach-Object -process {
    $k = [regex]::split($_, '=')
    if ($k.Length -ne 2) {
        return
    }
    $k[0] = $k[0].Trim()
    $k[1] = $k[1].Trim()
    if (($k[0].CompareTo("") -ne 0)) {
        $global:kubeovnConfig[$k[0]] = $k[1]
    }
}

$tunnelType = $($global:kubeovnConfig["TUNNEL_TYPE"]).ToLower()
$nodeName = $($global:kubeovnConfig["NODE_NAME"]).ToLower()
$kubeConfig = $global:kubeovnConfig["KUBE_CONFIG"]
$svcCidr = $global:kubeovnConfig["SVC_CIDR"]
$cniConfDir = $global:kubeovnConfig["CNI_CONF_DIR"]
$cniConfFile = $global:kubeovnConfig["CNI_CONF_FILE"]
$cniConfName = $global:kubeovnConfig["CNI_CONF_NAME"]
$enableMirror = $global:kubeovnConfig["ENABLE_MIRROR"]

$cmd = "C:\kube-ovn\bin\kube-ovn-daemon.exe " +
    "--enable-mirror=$enableMirror " +
    "--network-type=$tunnelType " +
    "--service-cluster-ip-range=$svcCidr " +
    "--kubeconfig=$kubeConfig " +
    "--cni-conf-dir=$cniConfDir " +
    "--cni-conf-file=$cniConfFile " +
    "--cni-conf-name=$cniConfName " +
    "--log_file=C:\kube-ovn\log\kube-ovn-cni.log " +
    "--log_file_max_size=100 " +
    "--logtostderr=false"

Invoke-Expression $cmd
