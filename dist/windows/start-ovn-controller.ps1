$ErrorActionPreference = 'Stop'

$ovnConfigPath = "C:\ovn\etc\ovn-controller.conf"

$global:ovnConfig = @{}
Get-Content $ovnConfigPath | ForEach-Object -process {
    $k = [regex]::split($_, '=')
    if ($k.Length -ne 2) {
        return
    }
    $k[0] = $k[0].Trim()
    $k[1] = $k[1].Trim()
    if (($k[0].CompareTo("") -ne 0)) {
        $global:ovnConfig[$k[0]] = $k[1]
    }
}

$ovnDBIPs = $global:ovnConfig["OVN_DB_IPS"]
$ovnDBPort = $global:ovnConfig["OVN_DB_PORT"]
$tunnelType = $($global:ovnConfig["TUNNEL_TYPE"]).ToLower()
$nodeName = $($global:ovnConfig["NODE_NAME"]).ToLower()
$hnsNetwork = $global:ovnConfig["HNS_NETWORK"]

$connections = @()
foreach ($ip in $ovnDBIPs.Split(',')) {
    $connections += [string]::Format("tcp:[{0}]:{1}", $ip, $ovnDBPort)
}
$connString = $connections -join ','

$systemId = ""
$systemIdConf = "C:\ovn\etc\system-id.conf"
if (Test-Path -Path $systemIdConf) {
    $systemId = $(Get-Content -Path $systemIdConf).Trim()
} else {
    $systemId = New-Guid | Select -ExpandProperty "Guid"
    Out-File -Encoding utf8 -InputObject $systemId -FilePath $systemIdConf
}

ovs-vsctl --may-exist add-br br-int -- set-controller br-int ptcp:6653:127.0.0.1

ovs-vsctl set open . external-ids:system-id="$systemId"
ovs-vsctl set open . external-ids:ovn-remote="$connString"
ovs-vsctl set open . external-ids:ovn-remote-probe-interval=10000
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval=180
ovs-vsctl set open . external-ids:ovn-encap-type="$tunnelType"
ovs-vsctl set open . external-ids:hostname="$nodeName"

$cmd = 'C:\ovn\usr\bin\ovn-controller.exe ' +
    '--log-file="C:\ovn\var\log\ovn-controller.log" ' +
    '--pidfile="C:\ovn\var\run\ovn\ovn-controller.pid" ' +
    'tcp:127.0.0.1:6640'

Invoke-Expression $cmd
